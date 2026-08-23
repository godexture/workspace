package audio

import (
	"math"
	"testing"
	"time"
)

func newCompressorKernel() filter {
	return &compressor{
		threshold: -20,
		ratio:     4,
		knee:      6,
		makeup:    1,
		attack:    timeConstant(5*time.Millisecond, 48_000),
		release:   timeConstant(50*time.Millisecond, 48_000),
	}
}

func TestCompressorKeepsItsEnvelopeAcrossFrames(t *testing.T) {
	chunkInvariant(t, newCompressorKernel, 2, 64)
}

// A level below the threshold is not the compressor's business, and one above
// it comes back reduced. Both are statements about the static curve rather
// than about how quickly the envelope reaches it, so the input is long enough
// for the envelope to settle.
func TestCompressorLeavesQuietSignalsAndReducesLoudOnes(t *testing.T) {
	quiet := steady(0.01, 4096)
	newCompressorKernel().Apply(quiet)
	if quiet[0][len(quiet[0])-1] != 0.01 {
		t.Fatalf("a signal 40 dB under the threshold came back as %v", quiet[0][len(quiet[0])-1])
	}

	loud := steady(1, 4096)
	newCompressorKernel().Apply(loud)
	settled := float64(loud[0][len(loud[0])-1])
	// Full scale is 20 dB over the threshold, and 4:1 leaves a quarter of that,
	// so the curve asks for 15 dB of reduction.
	want := math.Pow(10, -15.0/20)
	if math.Abs(settled-want) > 0.01 {
		t.Fatalf("full scale settled at %v, want about %v", settled, want)
	}
}

// The detector is linked across channels so that a stereo image cannot move
// when one side gets louder: both sides take the same reduction.
func TestCompressorReducesEveryChannelEqually(t *testing.T) {
	planes := [][]float32{make([]float32, 4096), make([]float32, 4096)}
	for index := range planes[0] {
		planes[0][index] = 1
		planes[1][index] = 0.001
	}
	newCompressorKernel().Apply(planes)
	loud := float64(planes[0][len(planes[0])-1])
	quiet := float64(planes[1][len(planes[1])-1])
	if math.Abs(loud/1-quiet/0.001) > 1e-3 {
		t.Fatalf("channels took different gains: %v and %v", loud/1, quiet/0.001)
	}
}

func newHardGateKernel() filter { return &hardGate{threshold: amplitude(-40)} }

func newLowpassGateKernel(channels int) filter {
	return &lowpassGate{
		threshold: -40,
		span:      40,
		attack:    timeConstant(5*time.Millisecond, 48_000),
		release:   timeConstant(50*time.Millisecond, 48_000),
		open:      20_000,
		close:     200,
		rate:      48_000,
		state:     make([]float32, channels),
	}
}

func TestLowpassGateKeepsItsEnvelopeAcrossFrames(t *testing.T) {
	chunkInvariant(t, func() filter { return newLowpassGateKernel(2) }, 2, 64)
}

// A hard gate silences a position only when every channel is quiet there, so a
// stereo pair never loses one side while the other plays on.
func TestHardGateSilencesOnlyWhereEveryChannelIsQuiet(t *testing.T) {
	planes := [][]float32{{0.5, 0.0001, 0.0001}, {0.0001, 0.5, 0.0001}}
	newHardGateKernel().Apply(planes)
	if planes[0][0] != 0.5 || planes[1][0] != 0.0001 {
		t.Fatalf("a position one channel was loud at was gated: %v", planes)
	}
	if planes[0][1] != 0.0001 || planes[1][1] != 0.5 {
		t.Fatalf("a position the other channel was loud at was gated: %v", planes)
	}
	if planes[0][2] != 0 || planes[1][2] != 0 {
		t.Fatalf("a position every channel was quiet at survived: %v", planes)
	}
}

func steady(level float32, samples int) [][]float32 {
	planes := [][]float32{make([]float32, samples)}
	for index := range planes[0] {
		planes[0][index] = level
	}
	return planes
}
