package audio

import (
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
)

func processedInputs(t *testing.T, rate int, length int64, stated bool) flow.Descriptors[stream.Descriptor] {
	t.Helper()
	signal := sample.Signal{Rate: rate, Layout: sample.Mono(), ValidBits: 32}
	properties, err := processed(signal).Properties()
	if err != nil {
		t.Fatal(err)
	}
	if stated {
		if properties, err = stream.WithDuration(properties, timing.NewDuration(length)); err != nil {
			t.Fatal(err)
		}
	}
	descriptor, err := stream.NewDescriptor("s", sample.Frames[float32]().Descriptor(), timing.MustBase(1, int64(rate)), properties)
	if err != nil {
		t.Fatal(err)
	}
	return flow.NewDescriptors(flow.Describe("frames", descriptor))
}

func compiledLength(t *testing.T, compiled flow.Descriptors[stream.Descriptor]) (int64, bool) {
	t.Helper()
	output, ok := compiled.One("filtered")
	if !ok {
		t.Fatal("compile produced no output descriptor")
	}
	value, stated := stream.DurationOf(output.Properties())
	return value.Value().Int64(), stated
}

// Interpolating leaves a different number of samples covering the same
// instants, so a length the input stated has to be restated rather than
// carried over as the count of samples that no longer exist.
func TestResampleRestatesAStatedLength(t *testing.T) {
	shape := filterShape()
	compiled, err := compileResample(shape,
		resampleConfig{Rate: config.FixedRate(24_000), MaxSamples: defaultFilterSamples},
		processedInputs(t, 48_000, 96_000, true))
	if err != nil {
		t.Fatal(err)
	}
	length, stated := compiledLength(t, compiled.Outputs)
	if !stated || length != 48_000 {
		t.Fatalf("length = %d, %v; want half of 96000", length, stated)
	}
}

// Relabelling moves no samples, so the same count of them still covers the
// stream however fast they are now said to pass.
func TestRelabellingKeepsTheLengthItWasGiven(t *testing.T) {
	shape := filterShape()
	compiled, err := compileRetime(shape,
		retimeConfig{Factor: 2, Mode: relabelRetime, MaxSamples: defaultFilterSamples},
		processedInputs(t, 48_000, 96_000, true))
	if err != nil {
		t.Fatal(err)
	}
	length, stated := compiledLength(t, compiled.Outputs)
	if !stated || length != 96_000 {
		t.Fatalf("length = %d, %v; want the count it arrived with", length, stated)
	}
}

// A stream that never stated a length does not acquire one by passing through.
func TestAStageDoesNotInventALengthNobodyStated(t *testing.T) {
	shape := filterShape()
	compiled, err := compileResample(shape,
		resampleConfig{Rate: config.FixedRate(24_000), MaxSamples: defaultFilterSamples},
		processedInputs(t, 48_000, 0, false))
	if err != nil {
		t.Fatal(err)
	}
	if length, stated := compiledLength(t, compiled.Outputs); stated {
		t.Fatalf("an unstated length came back as %d", length)
	}
}
