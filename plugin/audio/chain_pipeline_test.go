package filter

import (
	"context"
	"io"
	"math"
	"testing"
	"time"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/plugin/audio/internal/gain"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/engine"
)

// chainBlockSizes is the M0 "small/medium/large block" baseline shape from
// docs/refactor/checkpoint.md M0#5.
var chainBlockSizes = []struct {
	name   string
	frames int
}{
	{"Small", 64},
	{"Medium", 4096},
	{"Large", 65536},
}

const chainFrameCount = 8

// BenchmarkGainChainPipeline runs the 1/4/16-stage gain chain through a
// real core/pipeline.Pipeline (source -> N gain filter nodes -> sink),
// unlike BenchmarkGainChainDepths in chain_test.go, which calls
// SendFrame/ReceiveFrame directly and so measures only the filter kernels
// with none of the scheduler/edge/ownership machinery a production graph
// actually pays for. That direct-call benchmark is kept as the lower
// bound, and both benchmarks now exclude everything but their own
// steady-state processing (see chain_test.go's doc comment), so the gap
// between the two reported ns/op values is the pipeline's own overhead and
// nothing else.
//
// Construction and Prepare are excluded from the timed/measured portion:
// buildGainChainPipeline runs under b.StopTimer/b.StartTimer (which pauses
// both wall-clock and allocation counting, per testing.B), and Prepare is
// called explicitly in that same excluded window so Run's own internal
// Prepare call (Pipeline.Run always calls Prepare first) is a no-op by the
// time the timer restarts. BenchmarkGainChainPipelineOpen below isolates
// that excluded construction+Prepare cost on its own.
//
// b.N's own ns/op and allocs/op still include Run's teardown (every node's
// Close), since Pipeline has no public entry point that runs nodes without
// also closing them -- there is no way to StopTimer only around Close from
// outside Run. Instead, buildGainChainPipeline constructs the Pipeline via
// pipeline.NewObserved(pipeline.ObservationMetrics, ...), and this
// benchmark reads Snapshot() after each Run to get every node's
// Start(ctx)-only Elapsed window (which ends before teardown begins) and
// reports the true steady-state-only wall-clock time as a custom
// "processing-ns/op" metric, alongside the teardown-inclusive built-in
// ns/op.
func BenchmarkGainChainPipeline(b *testing.B) {
	for _, depth := range chainDepths {
		for _, size := range chainBlockSizes {
			b.Run(depthName(depth)+"/"+size.name, func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(size.frames * 2 * 4 * chainFrameCount))
				block := stereoBlock(size.frames)
				var totalProcessing time.Duration
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					conversion, sink := buildGainChainPipeline(b, depth, chainFrameCount, block)
					if err := conversion.Prepare(context.Background()); err != nil {
						b.Fatalf("Prepare() error = %v", err)
					}
					b.StartTimer()

					if err := conversion.Run(context.Background()); err != nil {
						b.Fatal(err)
					}

					b.StopTimer()
					totalProcessing += processingElapsed(conversion.Snapshot())
					if sink.decodeErr != nil {
						b.Fatalf("sink decode error: %v", sink.decodeErr)
					}
					if sink.frameCount != chainFrameCount {
						b.Fatalf("sink processed %d frames, want %d", sink.frameCount, chainFrameCount)
					}
					wantSamples := chainFrameCount * size.frames
					if sink.sampleCount != wantSamples {
						b.Fatalf("sink processed %d samples, want %d", sink.sampleCount, wantSamples)
					}
					b.StartTimer()
				}
				if b.N > 0 {
					b.ReportMetric(float64(totalProcessing.Nanoseconds())/float64(b.N), "processing-ns/op")
				}
			})
		}
	}
}

// processingElapsed reduces a Pipeline Snapshot taken after Run returns to
// a single steady-state duration: the span from the earliest node
// Start(ctx) began to the latest one finished. Nodes run concurrently (see
// core/pipeline's errgroup-based runNodesObservedSelected), so this is the
// wall-clock width of that concurrent window, not a sum across nodes --
// and since nodeMetrics.finish is always called before Run's teardown
// begins, it never includes Close.
func processingElapsed(snapshot pipeline.Snapshot) time.Duration {
	var start, finish time.Time
	for _, n := range snapshot.Nodes {
		if n.StartedAt.IsZero() {
			continue
		}
		if start.IsZero() || n.StartedAt.Before(start) {
			start = n.StartedAt
		}
		if n.FinishedAt.After(finish) {
			finish = n.FinishedAt
		}
	}
	if start.IsZero() || finish.IsZero() {
		return 0
	}
	return finish.Sub(start)
}

// BenchmarkGainChainPipelineOpen isolates construction+Prepare (cold
// lifecycle) cost: the counterpart to BenchmarkGainChainPipeline's
// steady-state-plus-teardown measurement above. The input block is built
// once outside the timed loop (it is never mutated, so every iteration can
// safely reuse it), and Close runs under b.StopTimer so its cost -- which
// belongs to neither "Open" nor the other benchmark's "steady-state +
// teardown" -- is excluded from both.
func BenchmarkGainChainPipelineOpen(b *testing.B) {
	for _, depth := range chainDepths {
		b.Run(depthName(depth), func(b *testing.B) {
			b.ReportAllocs()
			block := stereoBlock(chainBlockSizes[0].frames)
			for i := 0; i < b.N; i++ {
				conversion, _ := buildGainChainPipeline(b, depth, chainFrameCount, block)
				if err := conversion.Prepare(context.Background()); err != nil {
					b.Fatalf("Prepare() error = %v", err)
				}

				b.StopTimer()
				conversion.Close()
				b.StartTimer()
			}
		})
	}
}

// buildGainChainPipeline is the shared construction path for both
// benchmarks above and TestGainChainPipelineOutputCorrectness below (and is
// where a future second runtime implementation would plug in behind a
// variant selector, so they can be run AB/BA against each other under the
// same harness instead of independent, hand-compared suites). It takes the
// input block directly (rather than building one internally) so a
// correctness check can compare the pipeline's output against the exact
// values it fed in.
func buildGainChainPipeline(t testing.TB, depth, frames int, block audio.Block) (*pipeline.Pipeline, *chainSink) {
	t.Helper()
	source := newChainSource(frames, block)
	sink := newChainSink()
	stages := make([]node.Filter, depth)
	for i := range stages {
		item, err := gain.New(config.GainConfig{Decibels: chainStageDecibels})
		if err != nil {
			t.Fatalf("gain.New() error = %v", err)
		}
		stages[i] = engine.WrapFilter(item)
	}

	nodes := make([]node.Node, 0, depth+2)
	nodes = append(nodes, source)
	prev, prevPort := node.OutputNode[media.Frame](source), "out"
	for _, stage := range stages {
		if err := pipeline.Link(prev, prevPort, stage, "in"); err != nil {
			t.Fatalf("Link() error = %v", err)
		}
		nodes = append(nodes, stage)
		prev, prevPort = stage, "out"
	}
	if err := pipeline.Link(prev, prevPort, sink, "in"); err != nil {
		t.Fatalf("Link() error = %v", err)
	}
	nodes = append(nodes, sink)

	// ObservationMetrics (rather than pipeline.New's default ObservationOff)
	// so BenchmarkGainChainPipeline can read each node's Start(ctx)-only
	// Elapsed via Snapshot() after Run returns -- see processingElapsed.
	conversion, err := pipeline.NewObserved(pipeline.ObservationMetrics, nodes...)
	if err != nil {
		t.Fatalf("pipeline.NewObserved() error = %v", err)
	}
	return conversion, sink
}

// TestProcessingElapsedSpansConcurrentNodesExcludingGaps pins
// processingElapsed's reduction rule directly against synthetic
// timestamps, independent of any real Pipeline run: it must be the span
// from the earliest StartedAt to the latest FinishedAt (nodes run
// concurrently, so summing would overcount), and must ignore nodes with a
// zero StartedAt (never observed to run).
func TestProcessingElapsedSpansConcurrentNodesExcludingGaps(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	snapshot := pipeline.Snapshot{Nodes: []pipeline.NodeSnapshot{
		{StartedAt: base.Add(2 * time.Millisecond), FinishedAt: base.Add(30 * time.Millisecond)},
		{StartedAt: base, FinishedAt: base.Add(10 * time.Millisecond)},
		{}, // unobserved node: zero StartedAt/FinishedAt, must not zero out the result
	}}
	got := processingElapsed(snapshot)
	want := 30 * time.Millisecond
	if got != want {
		t.Fatalf("processingElapsed() = %v, want %v", got, want)
	}
}

// TestGainChainPipelineOutputCorrectness drives the same gain chain the
// benchmarks above exercise, but asserts the pipeline's actual output
// values against the expected per-stage attenuation -- the benchmarks only
// ever checked frame/sample counts, never that Pipeline-mediated Run
// computed the right numbers (docs/refactor/checkpoint.md M0-R2).
func TestGainChainPipelineOutputCorrectness(t *testing.T) {
	t.Parallel()
	stageFactor := float64(math.Pow(10, chainStageDecibels/20))

	for _, depth := range chainDepths {
		for _, size := range chainBlockSizes {
			t.Run(depthName(depth)+"/"+size.name, func(t *testing.T) {
				t.Parallel()
				block := stereoBlock(size.frames)
				conversion, sink := buildGainChainPipeline(t, depth, chainFrameCount, block)
				sink.capture = true

				if err := conversion.Run(context.Background()); err != nil {
					t.Fatalf("Run() error = %v", err)
				}
				if sink.decodeErr != nil {
					t.Fatalf("sink decode error: %v", sink.decodeErr)
				}
				if sink.frameCount != chainFrameCount {
					t.Fatalf("frameCount = %d, want %d", sink.frameCount, chainFrameCount)
				}
				if len(sink.captured) != chainFrameCount {
					t.Fatalf("captured %d frames, want %d", len(sink.captured), chainFrameCount)
				}

				gain := float32(math.Pow(stageFactor, float64(depth)))
				for f, got := range sink.captured {
					if len(got) != len(block.Channels) {
						t.Fatalf("frame %d: channel count = %d, want %d", f, len(got), len(block.Channels))
					}
					for ch := range block.Channels {
						if len(got[ch]) != size.frames {
							t.Fatalf("frame %d channel %d: sample count = %d, want %d", f, ch, len(got[ch]), size.frames)
						}
						for i, v := range block.Channels[ch] {
							want := v * gain
							if math.Abs(float64(got[ch][i]-want)) > 1e-4 {
								t.Fatalf("frame %d channel %d sample %d = %g, want %g", f, ch, i, got[ch][i], want)
							}
						}
					}
				}
			})
		}
	}
}

// chainSource emits a fixed number of frames of a fixed input block, once.
type chainSource struct {
	out    *node.OutPort[media.Frame]
	frames int
	block  audio.Block
}

func newChainSource(frames int, block audio.Block) *chainSource {
	return &chainSource{
		out: node.NewOutPort[media.Frame]("out", media.StreamInfo{
			Type: media.MediaAudio,
			MediaAttributes: media.MediaAttributes{
				Audio: media.AudioAttributes{SampleRate: 48000, Format: media.SampleFormatF32P, ChannelLayout: media.LayoutStereo2_0},
			},
		}),
		frames: frames, block: block,
	}
}

func (s *chainSource) OutputPorts() map[string]*node.OutPort[media.Frame] {
	return map[string]*node.OutPort[media.Frame]{"out": s.out}
}
func (s *chainSource) Close() error { return nil }
func (s *chainSource) Start(ctx context.Context) error {
	out := s.out.Edge()
	defer out.Close()
	for i := 0; i < s.frames; i++ {
		encoded, err := audio.Encode(s.block, media.SampleFormatF32P, 32)
		if err != nil {
			return err
		}
		if err := out.Push(ctx, encoded); err != nil {
			return err
		}
	}
	return nil
}

// chainSink counts frames and samples actually delivered (the "processed
// frame/sample count" the checkpoint asks the benchmark to record) and
// records the first decode error rather than silently swallowing it, so a
// corrupted stream shows up as a hard failure instead of a quietly short
// sample count. It discards decoded data by default (so the performance
// benchmarks' allocation counts aren't inflated by holding every block
// live); setting capture retains a deep copy of each frame's channels for
// TestGainChainPipelineOutputCorrectness to compare against the expected
// output.
type chainSink struct {
	in          *node.InPort[media.Frame]
	frameCount  int
	sampleCount int
	decodeErr   error
	capture     bool
	captured    [][][]float32
}

func newChainSink() *chainSink { return &chainSink{in: node.NewInPort[media.Frame]("in")} }

func (s *chainSink) InputPorts() map[string]*node.InPort[media.Frame] {
	return map[string]*node.InPort[media.Frame]{"in": s.in}
}
func (s *chainSink) Close() error { return nil }
func (s *chainSink) Start(ctx context.Context) error {
	in := s.in.Edge()
	for {
		frame, err := in.Pull(ctx)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		s.frameCount++
		block, decodeErr := audio.Decode(&frame)
		if decodeErr != nil {
			if s.decodeErr == nil {
				s.decodeErr = decodeErr
			}
			frame.Release()
			continue
		}
		if len(block.Channels) > 0 {
			s.sampleCount += len(block.Channels[0])
		}
		if s.capture {
			s.captured = append(s.captured, cloneChannels(block.Channels))
		}
		frame.Release()
	}
}

func cloneChannels(channels [][]float32) [][]float32 {
	cloned := make([][]float32, len(channels))
	for i, ch := range channels {
		cloned[i] = append([]float32(nil), ch...)
	}
	return cloned
}
