package filter

import (
	"context"
	"io"
	"testing"

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
// bound; the gap between the two is the pipeline's own overhead.
//
// Construction/config/Open is measured separately from steady-state Run in
// BenchmarkGainChainPipelineOpen below, so a regression in one is not
// hidden by (or blamed on) the other.
func BenchmarkGainChainPipeline(b *testing.B) {
	for _, depth := range chainDepths {
		for _, size := range chainBlockSizes {
			b.Run(depthName(depth)+"/"+size.name, func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(size.frames * 2 * 4 * chainFrameCount))
				for i := 0; i < b.N; i++ {
					conversion, sink := buildGainChainPipeline(b, depth, chainFrameCount, size.frames)
					if err := conversion.Run(context.Background()); err != nil {
						b.Fatal(err)
					}
					if sink.frameCount != chainFrameCount {
						b.Fatalf("sink processed %d frames, want %d", sink.frameCount, chainFrameCount)
					}
				}
			})
		}
	}
}

// BenchmarkGainChainPipelineOpen isolates construction+Prepare/Open cost
// (the pipeline is built but never Run), so it can be compared against
// BenchmarkGainChainPipeline's construction-included total to see how much
// of steady-state throughput is actually startup cost, especially at
// Small block sizes where a short Run barely amortizes it.
func BenchmarkGainChainPipelineOpen(b *testing.B) {
	for _, depth := range chainDepths {
		b.Run(depthName(depth), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				conversion, _ := buildGainChainPipeline(b, depth, chainFrameCount, chainBlockSizes[0].frames)
				conversion.Close()
			}
		})
	}
}

// buildGainChainPipeline is the shared construction path for both
// benchmarks above (and is where a future second runtime implementation
// would plug in behind a variant selector, so the two can be run AB/BA
// against each other under the same harness instead of two independent,
// hand-compared benchmark suites).
func buildGainChainPipeline(b *testing.B, depth, frames, blockSize int) (*pipeline.Pipeline, *chainSink) {
	b.Helper()
	source := newChainSource(frames, blockSize)
	sink := newChainSink()
	stages := make([]node.Filter, depth)
	for i := range stages {
		item, err := gain.New(config.GainConfig{Decibels: chainStageDecibels})
		if err != nil {
			b.Fatalf("gain.New() error = %v", err)
		}
		stages[i] = engine.WrapFilter(item)
	}

	nodes := make([]node.Node, 0, depth+2)
	nodes = append(nodes, source)
	prev, prevPort := node.OutputNode[media.Frame](source), "out"
	for _, stage := range stages {
		if err := pipeline.Link(prev, prevPort, stage, "in"); err != nil {
			b.Fatalf("Link() error = %v", err)
		}
		nodes = append(nodes, stage)
		prev, prevPort = stage, "out"
	}
	if err := pipeline.Link(prev, prevPort, sink, "in"); err != nil {
		b.Fatalf("Link() error = %v", err)
	}
	nodes = append(nodes, sink)

	conversion, err := pipeline.New(nodes...)
	if err != nil {
		b.Fatalf("pipeline.New() error = %v", err)
	}
	return conversion, sink
}

// chainSource emits a fixed number of frames of a fixed block size, once.
type chainSource struct {
	out       *node.OutPort[media.Frame]
	frames    int
	blockSize int
}

func newChainSource(frames, blockSize int) *chainSource {
	return &chainSource{
		out: node.NewOutPort[media.Frame]("out", media.StreamInfo{
			Type: media.MediaAudio,
			MediaAttributes: media.MediaAttributes{
				Audio: media.AudioAttributes{SampleRate: 48000, Format: media.SampleFormatF32P, ChannelLayout: media.LayoutStereo2_0},
			},
		}),
		frames: frames, blockSize: blockSize,
	}
}

func (s *chainSource) OutputPorts() map[string]*node.OutPort[media.Frame] {
	return map[string]*node.OutPort[media.Frame]{"out": s.out}
}
func (s *chainSource) Close() error { return nil }
func (s *chainSource) Start(ctx context.Context) error {
	out := s.out.Edge()
	defer out.Close()
	block := stereoBlock(s.blockSize)
	for i := 0; i < s.frames; i++ {
		encoded, err := audio.Encode(block, media.SampleFormatF32P, 32)
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
// frame/sample count" the checkpoint asks the benchmark to record) while
// discarding the data.
type chainSink struct {
	in          *node.InPort[media.Frame]
	frameCount  int
	sampleCount int
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
		if block, decodeErr := audio.Decode(&frame); decodeErr == nil && len(block.Channels) > 0 {
			s.sampleCount += len(block.Channels[0])
		}
		frame.Release()
	}
}
