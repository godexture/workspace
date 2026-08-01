package pipeline

import (
	"context"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/godexture/godec/core/domain/media"
)

// callArgs matches the "(0x1234, 0xabcd?)" argument-value list runtime.Stack
// prints after each function name. Those values are per-call register/stack
// dumps (pointer addresses, iteration counts, ...) that differ on every
// capture even for the exact same call site, so they must be stripped
// before comparing two stack dumps for content equality.
var callArgs = regexp.MustCompile(`\([^)]*\)`)

// TestPipelineObservationDoesNotLeakGoroutines is the M0 baseline for
// docs/refactor/checkpoint.md M0#6 (extending docs/refactor/quality.md's
// "observation off/on の ... goroutine profile" item): each observation
// mode (and the Plain, unobserved path) must return to the pre-run
// goroutine set after Run() completes, run after run, and must process
// every packet the source actually sent.
//
// This checks more than an aggregate runtime.NumGoroutine() delta:
//   - convergence is polled with a bounded deadline instead of sampled
//     once after a fixed sleep, so a goroutine that unwinds slightly late
//     doesn't read as a leak;
//   - the *set* of goroutine stacks is diffed, not just their count, so a
//     leaked goroutine can't hide behind an unrelated goroutine happening
//     to exit around the same time;
//   - the sink's processed packet count is asserted for every mode, so a
//     variant that silently drops packets can't look "faster" or
//     "leak-free" by doing less work.
func TestPipelineObservationDoesNotLeakGoroutines(t *testing.T) {
	const packets = 64
	const packetSize = 4096

	variants := []struct {
		name  string
		plain bool
		mode  ObservationMode
	}{
		{name: "Plain", plain: true},
		{name: "Off", mode: ObservationOff},
		{name: "Progress", mode: ObservationProgress},
		{name: "Metrics", mode: ObservationMetrics},
	}

	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			before := settledStacks(t)

			for range 5 {
				conversion, sink, err := buildObservationVariant(variant.plain, variant.mode, packets, packetSize)
				if err != nil {
					t.Fatalf("build pipeline: %v", err)
				}
				if err := conversion.Run(context.Background()); err != nil {
					t.Fatalf("Run(): %v", err)
				}
				if sink.count != packets {
					t.Fatalf("sink processed %d packets, want %d (a dropped-item speedup is not a pass)", sink.count, packets)
				}
			}

			after := settledStacks(t)
			if leaked := newStacks(before, after); len(leaked) > 0 {
				t.Fatalf("%d goroutine stack(s) present after 5 runs that were not present before:\n%s",
					len(leaked), strings.Join(leaked, "\n---\n"))
			}
		})
	}
}

func buildObservationVariant(plain bool, mode ObservationMode, packets, packetSize int) (*Pipeline, *observationSink, error) {
	stream := media.StreamInfo{Index: 0, Type: media.MediaAudio}
	source := newObservationSource(packets, packetSize, stream)
	sink := newObservationSink()

	if plain {
		if err := LinkWithBufferSize(source, "out", sink, "in", 100); err != nil {
			return nil, nil, err
		}
		conversion, err := New(source, sink)
		return conversion, sink, err
	}

	geometry := NewGeometry()
	if err := geometry.AddNodeDef(NodeDef{ID: "source", Node: source, Description: NodeDescription{Outputs: []media.StreamInfo{stream}}}); err != nil {
		return nil, nil, err
	}
	if err := geometry.AddNodeDef(NodeDef{ID: "sink", Node: sink, Description: NodeDescription{Inputs: []media.StreamInfo{stream}}}); err != nil {
		_ = geometry.Close()
		return nil, nil, err
	}
	if err := geometry.AddEdgeDef(EdgeDef{FromNode: "source", FromPort: "out", ToNode: "sink", ToPort: "in", Stream: stream, ProgressSource: true}); err != nil {
		_ = geometry.Close()
		return nil, nil, err
	}
	conversion, err := NewBuilder().Build(geometry, WithObservation(mode))
	return conversion, sink, err
}

// settledStacks polls runtime.Stack until the goroutine count stops
// dropping (recently-finished goroutines have unwound) or a bounded
// deadline elapses, then returns each remaining goroutine's own stack
// trace as an independent element, keyed by its content -- not by count --
// so the caller can diff *which* goroutines are new, not just how many.
func settledStacks(t *testing.T) map[string]int {
	t.Helper()
	const deadline = 2 * time.Second
	const pollInterval = 10 * time.Millisecond

	deadlineAt := time.Now().Add(deadline)
	last := -1
	for time.Now().Before(deadlineAt) {
		runtime.Gosched()
		runtime.GC()
		n := runtime.NumGoroutine()
		if n == last {
			break
		}
		last = n
		time.Sleep(pollInterval)
	}
	return goroutineStacks(t)
}

func goroutineStacks(t *testing.T) map[string]int {
	t.Helper()
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	stacks := map[string]int{}
	for _, block := range strings.Split(string(buf), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		// Skip the calling goroutine's own stack: it is necessarily mid-
		// call inside goroutineStacks/settledStacks at the moment of
		// capture, which is not stable to self-capture (its exact
		// resumption point can differ run to run for reasons unrelated to
		// any real leak) and is test-harness machinery, not a pipeline
		// goroutine this test cares about anyway.
		if strings.Contains(block, "pipeline.goroutineStacks(") {
			continue
		}
		// Drop the "goroutine N [state]:" line: the numeric ID is unique
		// per goroutine even for structurally identical stacks, which
		// would make every occurrence look "new". Also strip each frame's
		// argument-value list (see callArgs): those are per-call dumps
		// that differ even for the same call site.
		if idx := strings.IndexByte(block, '\n'); idx >= 0 {
			block = block[idx+1:]
		}
		block = callArgs.ReplaceAllString(block, "(...)")
		stacks[block]++
	}
	return stacks
}

// newStacks returns, as formatted text, every stack in after with a higher
// occurrence count than the same stack had in before -- goroutines that
// exist now but didn't (or didn't as many times) beforehand.
func newStacks(before, after map[string]int) []string {
	var leaked []string
	for stack, afterCount := range after {
		if afterCount > before[stack] {
			leaked = append(leaked, stack)
		}
	}
	return leaked
}
