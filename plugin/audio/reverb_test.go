package audio

import (
	"testing"

	"github.com/godexture/godec/media/sample"
)

func newReverbKernel(channels int, wet, dry float32) filter {
	signal := sample.Signal{Rate: 48_000, Layout: sample.Channels(channels), ValidBits: 32}
	result := &reverb{wet: wet, dry: dry, networks: make([]reverbNetwork, channels)}
	for channel := range result.networks {
		result.networks[channel] = newReverbNetwork(channel, signal.Rate, roomOffset+0.5*roomScale, 0.5*dampScale)
	}
	return result
}

func TestReverbKeepsItsNetworksAcrossFrames(t *testing.T) {
	chunkInvariant(t, func() filter { return newReverbKernel(2, 1, 1) }, 2, 64)
}

// An impulse has to leave a tail behind it, and the tail has to fall away
// rather than sustain: those two together are what separates a reverb from a
// delay line that never empties.
func TestReverbLeavesADecayingTail(t *testing.T) {
	planes := [][]float32{make([]float32, 48_000)}
	planes[0][0] = 1
	newReverbKernel(1, 1, 0).Apply(planes)

	early := peakOf(planes[0][:8_000])
	late := peakOf(planes[0][40_000:])
	if early == 0 {
		t.Fatal("an impulse left no tail")
	}
	if late >= early {
		t.Fatalf("the tail did not decay: %v early, %v late", early, late)
	}
}

// Each channel is offset so that a multi-channel input decorrelates instead of
// every channel reverberating identically.
func TestReverbGivesEachChannelItsOwnTail(t *testing.T) {
	planes := [][]float32{make([]float32, 4_096), make([]float32, 4_096)}
	planes[0][0], planes[1][0] = 1, 1
	newReverbKernel(2, 1, 0).Apply(planes)

	identical := true
	for index := range planes[0] {
		if planes[0][index] != planes[1][index] {
			identical = false
			break
		}
	}
	if identical {
		t.Fatal("both channels produced the same tail")
	}
}

// With no wet level there is nothing to add, so the samples come back scaled
// by the dry level and nothing else.
func TestReverbWithNoWetLevelOnlyScales(t *testing.T) {
	planes := [][]float32{tone(1_000, 48_000, 64)}
	want := clonePlanes(planes)
	newReverbKernel(1, 0, 0.5).Apply(planes)
	for index := range planes[0] {
		if planes[0][index] != want[0][index]*0.5 {
			t.Fatalf("sample %d = %v, want %v", index, planes[0][index], want[0][index]*0.5)
		}
	}
}
