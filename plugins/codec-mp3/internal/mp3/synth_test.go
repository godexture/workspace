package mp3

import (
	"slices"
	"testing"

	"github.com/godexture/codec-mp3/internal/mp3/layer3"
)

func TestSynthesizeGranuleChannelTransitions(t *testing.T) {
	for _, test := range []struct {
		name      string
		bandCount int
	}{
		{name: "layer3", bandCount: SamplesPerSubBandLayer3},
		{name: "layer12", bandCount: SamplesPerSubBandLayer12},
	} {
		t.Run(test.name, func(t *testing.T) {
			var decoder Decoder
			decoder.Init()
			var referenceState [synthHistoryLength]float32
			var referenceWorkspace [2112]float32

			for step, channels := range []int{2, 1, 1, 2, 1, 2} {
				input := make([]float32, layer3.SamplesPerGranule*MaxChannels)
				for i := range input {
					input[i] = float32((step+1)*(i%31-15)) / 16
				}
				gotInput := slices.Clone(input)
				wantInput := slices.Clone(input)
				got := make([]float32, layer3.SamplesPerGranule*MaxChannels)
				want := make([]float32, layer3.SamplesPerGranule*MaxChannels)

				decoder.synthesizeGranule(gotInput, test.bandCount, channels, got)
				referenceSynthesizeGranule(referenceState[:], wantInput, test.bandCount, channels, want, referenceWorkspace[:])

				if !slices.Equal(got, want) {
					for i := range got {
						if got[i] != want[i] {
							t.Fatalf("step %d, channels %d, sample %d: got %v, want %v", step, channels, i, got[i], want[i])
						}
					}
				}
			}
			if decoder.synthesis.Cap() != synthBufferLength {
				t.Fatalf("synthesis capacity grew to %d", decoder.synthesis.Cap())
			}
		})
	}
}

func referenceSynthesizeGranule(state, granule []float32, bandCount, channelCount int, pcm, workspace []float32) {
	for i := 0; i < channelCount; i++ {
		dctType2(granule[layer3.SamplesPerGranule*i:], bandCount)
	}
	copy(workspace[:synthHistoryLength], state)
	for i := 0; i < bandCount; i += 2 {
		synthesizeFloat(granule[i:], pcm[32*channelCount*i:], channelCount, workspace[i*64:])
	}
	if channelCount == 1 {
		for i := 0; i < synthHistoryLength; i += 2 {
			state[i] = workspace[bandCount*64+i]
		}
	} else {
		copy(state, workspace[bandCount*64:bandCount*64+synthHistoryLength])
	}
}
