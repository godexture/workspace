package decode

import (
	"fmt"
	"math"
	"slices"
	"testing"
)

func TestDCTZero(t *testing.T) {
	for _, bandCount := range []int{0, 12, 18} {
		t.Run(fmt.Sprintf("bands=%d", bandCount), func(t *testing.T) {
			var granule [granuleSamples]float32
			dct(granule[:], bandCount)
			for index, sample := range granule {
				if sample != 0 {
					t.Fatalf("sample %d = %v, want zero", index, sample)
				}
			}
		})
	}
}

func TestDCTMatchesDefinition(t *testing.T) {
	for _, bandCount := range []int{12, 18} {
		t.Run(fmt.Sprintf("bands=%d", bandCount), func(t *testing.T) {
			granule := patternedGranule(bandCount)
			original := granule
			dct(granule[:granuleSamples], bandCount)

			for band := 0; band < bandCount; band++ {
				for coefficient := 0; coefficient < subbands; coefficient++ {
					var want float64
					for sample := 0; sample < subbands; sample++ {
						angle := math.Pi / subbands * (float64(sample) + 0.5) * float64(coefficient)
						want += float64(original[sample*granuleStride+band]) * math.Cos(angle)
					}
					got := float64(granule[coefficient*granuleStride+band])
					tolerance := 1e-4 * math.Max(1, math.Abs(want))
					if math.Abs(got-want) > tolerance {
						t.Fatalf("band %d, coefficient %d = %v, want %v (tolerance %v)", band, coefficient, got, want, tolerance)
					}
				}
			}
			for index := bandCount; index < granuleStride; index++ {
				for sample := 0; sample < subbands; sample++ {
					offset := sample*granuleStride + index
					if granule[offset] != original[offset] {
						t.Fatalf("unselected band %d, sample %d changed", index, sample)
					}
				}
			}
		})
	}
}

func TestSynthesizerZeroInput(t *testing.T) {
	for _, channelCount := range []int{1, 2} {
		t.Run(fmt.Sprintf("channels=%d", channelCount), func(t *testing.T) {
			var synthesizer synthesizer
			var granule [granuleSamples * channels]float32
			var samples [granuleSamples * channels]float32
			for i := range samples {
				samples[i] = 1
			}

			synthesizer.synthesize(granule[:], granuleStride, channelCount, samples[:granuleSamples*channelCount])
			for index, sample := range samples[:granuleSamples*channelCount] {
				if sample != 0 {
					t.Fatalf("sample %d = %v, want zero", index, sample)
				}
			}
			if synthesizer.history != [historySamples]float32{} {
				t.Fatal("zero input changed history")
			}
		})
	}
}

func TestSynthesizerReset(t *testing.T) {
	var used, fresh synthesizer
	input := patternedGranule(1)
	var discarded [granuleSamples * channels]float32
	used.synthesize(input[:], granuleStride, channels, discarded[:])
	if used.history == [historySamples]float32{} {
		t.Fatal("patterned input did not change history")
	}

	used.reset()
	if used.history != [historySamples]float32{} || used.workspace != [workspaceSamples]float32{} {
		t.Fatal("reset retained synthesis state")
	}

	gotInput := patternedGranule(2)
	wantInput := gotInput
	var got, want [granuleSamples * channels]float32
	used.synthesize(gotInput[:], granuleStride, channels, got[:])
	fresh.synthesize(wantInput[:], granuleStride, channels, want[:])
	if !slices.Equal(got[:], want[:]) || used.history != fresh.history {
		t.Fatal("reset synthesizer differs from a fresh synthesizer")
	}
}

func TestSynthesizerReusesFixedState(t *testing.T) {
	var synthesizer synthesizer
	input := patternedGranule(3)
	var samples [granuleSamples * channels]float32
	for range 64 {
		current := input
		synthesizer.synthesize(current[:], granuleStride, channels, samples[:])
	}
	if got, want := len(synthesizer.history), historySamples; got != want {
		t.Fatalf("history length = %d, want %d", got, want)
	}
	if got, want := len(synthesizer.workspace), workspaceSamples; got != want {
		t.Fatalf("workspace length = %d, want %d", got, want)
	}

	allocations := testing.AllocsPerRun(100, func() {
		current := input
		synthesizer.synthesize(current[:], granuleStride, channels, samples[:])
	})
	if allocations != 0 {
		t.Fatalf("allocations per synthesis = %v, want zero", allocations)
	}
}

func TestSynthesizerChannelTransitions(t *testing.T) {
	for _, bandCount := range []int{12, 18} {
		t.Run(fmt.Sprintf("bands=%d", bandCount), func(t *testing.T) {
			var got synthesizer
			var want freshWorkspaceSynthesizer
			for step, channelCount := range []int{2, 1, 1, 2, 1, 2} {
				gotInput := patternedGranule(step)
				wantInput := gotInput
				before := got.history
				var gotSamples, wantSamples [granuleSamples * channels]float32

				got.synthesize(gotInput[:], bandCount, channelCount, gotSamples[:bandCount*subbands*channelCount])
				want.synthesize(wantInput[:], bandCount, channelCount, wantSamples[:bandCount*subbands*channelCount])

				if !slices.Equal(gotInput[:], wantInput[:]) {
					t.Fatalf("step %d transformed granule differs", step)
				}
				if !slices.Equal(gotSamples[:bandCount*subbands*channelCount], wantSamples[:bandCount*subbands*channelCount]) {
					t.Fatalf("step %d, channels %d: samples differ", step, channelCount)
				}
				if got.history != want.history {
					t.Fatalf("step %d, channels %d: history differs", step, channelCount)
				}
				if channelCount == 1 {
					for i := 1; i < historySamples; i += 2 {
						if got.history[i] != before[i] {
							t.Fatalf("step %d: mono synthesis replaced right history at %d", step, i)
						}
					}
				}
			}
		})
	}
}

type freshWorkspaceSynthesizer struct {
	history [historySamples]float32
}

func (s *freshWorkspaceSynthesizer) synthesize(granule []float32, bandCount, channelCount int, samples []float32) {
	for channel := 0; channel < channelCount; channel++ {
		dct(granule[granuleSamples*channel:], bandCount)
	}
	var workspace [workspaceSamples]float32
	copy(workspace[:historySamples], s.history[:])
	for band := 0; band < bandCount; band += 2 {
		synthesizeFloat(granule[band:], samples[32*channelCount*band:], channelCount, workspace[band*64:])
	}
	state := workspace[bandCount*64 : bandCount*64+historySamples]
	if channelCount == 1 {
		for i := 0; i < historySamples; i += 2 {
			s.history[i] = state[i]
		}
		return
	}
	copy(s.history[:], state)
}

func patternedGranule(step int) [granuleSamples * channels]float32 {
	var granule [granuleSamples * channels]float32
	for i := range granule {
		granule[i] = float32((step+1)*(i%31-15)) / 16
	}
	return granule
}
