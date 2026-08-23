package audio

import (
	"math"
	"testing"

	"github.com/godexture/godec/media/sample"
)

func equalizerSignal(rate int) sample.Signal {
	return sample.Signal{Rate: rate, Layout: sample.Stereo(), ValidBits: 32}
}

func TestEqualizerKeepsItsSectionsAcrossFrames(t *testing.T) {
	bands := []equalizerBand{
		{Type: peakingBand, Frequency: 1_000, Gain: 6},
		{Type: lowShelfBand, Frequency: 200, Gain: -6, Q: 0.7},
	}
	chunkInvariant(t, func() filter { return newEqualizerKernel(bands, equalizerSignal(48_000)) }, 2, 64)
}

// A band asks for a level change at its own frequency, so that is where the
// response has to land. Measuring it needs a settled tone rather than a single
// frame, because a biquad reaches its steady state over a few periods.
func TestAPeakingBandChangesTheLevelAtItsOwnFrequency(t *testing.T) {
	const rate = 48_000
	bands := []equalizerBand{{Type: peakingBand, Frequency: 1_000, Gain: 6, Q: 4}}
	for _, test := range []struct {
		frequency float64
		want      float64
	}{
		{frequency: 1_000, want: math.Pow(10, 6.0/20)},
		{frequency: 60, want: 1},
	} {
		planes := [][]float32{tone(test.frequency, rate, rate)}
		newEqualizerKernel(bands, sample.Signal{Rate: rate, Layout: sample.Mono(), ValidBits: 32}).Apply(planes)
		got := float64(peakOf(planes[0][rate/2:]))
		if math.Abs(got-test.want) > 0.02 {
			t.Fatalf("%.0f Hz came back at %v, want about %v", test.frequency, got, test.want)
		}
	}
}

// The bands run in ascending frequency however they were written down, so two
// orderings of the same cascade are the same filter.
func TestTheCascadeDoesNotDependOnTheOrderBandsWereWrittenIn(t *testing.T) {
	const rate = 48_000
	first := []equalizerBand{
		{Type: peakingBand, Frequency: 4_000, Gain: -4, Q: 2},
		{Type: peakingBand, Frequency: 250, Gain: 5, Q: 1},
	}
	second := []equalizerBand{first[1], first[0]}
	planes := [][]float32{tone(1_000, rate, 512)}
	other := clonePlanes(planes)
	newEqualizerKernel(first, sample.Signal{Rate: rate, Layout: sample.Mono(), ValidBits: 32}).Apply(planes)
	newEqualizerKernel(second, sample.Signal{Rate: rate, Layout: sample.Mono(), ValidBits: 32}).Apply(other)
	for index := range planes[0] {
		if planes[0][index] != other[0][index] {
			t.Fatalf("sample %d differs between orderings: %v and %v", index, planes[0][index], other[0][index])
		}
	}
}

// A lone band spans an octave, and neighbours narrow each other, so adding a
// band beside one has to raise the width the pair is given.
func TestDerivedWidthsNarrowAsBandsCrowd(t *testing.T) {
	alone := derivedQ([]float64{1_000}, 0)
	if math.Abs(alone-math.Sqrt2) > 1e-9 {
		t.Fatalf("a lone band got Q %v, want an octave", alone)
	}
	crowded := derivedQ([]float64{500, 1_000, 2_000}, 1)
	if crowded <= alone {
		t.Fatalf("a crowded band got Q %v, no narrower than a lone one at %v", crowded, alone)
	}
}

// A band asked for no level change runs no section, so the samples come back
// exactly as they arrived rather than five coefficients later. It still counts
// toward the axis, because a slider left at zero is what its neighbours
// measure their widths against.
func TestABandWithNoGainRunsNoSectionAndStillShapesItsNeighbours(t *testing.T) {
	signal := sample.Signal{Rate: 48_000, Layout: sample.Mono(), ValidBits: 32}
	flat := newEqualizerKernel([]equalizerBand{
		{Type: peakingBand, Frequency: 1_000},
		{Type: lowShelfBand, Frequency: 200},
	}, signal)
	if len(flat.sections) != 0 {
		t.Fatalf("sections = %d, want none for bands that change nothing", len(flat.sections))
	}
	planes := [][]float32{tone(1_000, 48_000, 64)}
	want := clonePlanes(planes)
	flat.Apply(planes)
	for index := range planes[0] {
		if planes[0][index] != want[0][index] {
			t.Fatalf("sample %d moved to %v from %v", index, planes[0][index], want[0][index])
		}
	}

	// The 500 Hz band is alone in one cascade and has a silent neighbour in the
	// other, so only the axis can explain a different width.
	alone := newEqualizerKernel([]equalizerBand{{Type: peakingBand, Frequency: 500, Gain: 6}}, signal)
	beside := newEqualizerKernel([]equalizerBand{
		{Type: peakingBand, Frequency: 500, Gain: 6},
		{Type: peakingBand, Frequency: 600},
	}, signal)
	if len(beside.sections) != 1 {
		t.Fatalf("sections = %d, want only the band with gain", len(beside.sections))
	}
	if alone.sections[0] == beside.sections[0] {
		t.Fatal("a silent neighbour left the axis unchanged")
	}
}

func tone(frequency float64, rate, samples int) []float32 {
	result := make([]float32, samples)
	for index := range result {
		result[index] = float32(math.Sin(2 * math.Pi * frequency * float64(index) / float64(rate)))
	}
	return result
}

func peakOf(samples []float32) float32 {
	var result float32
	for _, value := range samples {
		if value < 0 {
			value = -value
		}
		if value > result {
			result = value
		}
	}
	return result
}
