package filter

import (
	"testing"

	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/plugin/audio/internal/equalizer"
)

func TestEqualizerPeakingZeroGainIsIdentity(t *testing.T) {
	item, err := equalizer.New(config.EqualizerConfig{EqualizerMode: config.EqualizerModeSingle, Type: config.EqualizerTypePeaking, FrequencyHz: 1000, Q: 0.7071067811865476})
	if err != nil {
		t.Fatal(err)
	}
	values := []float32{0.2, -0.5, 0.8, -0.1, 0.05, 0.9}
	send(t, item, frame(48000, 0, values))
	assertSamplesTol(t, receive(t, item), values, 1e-4)
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}

func TestEqualizerMultibandAllZeroGainsIsIdentity(t *testing.T) {
	item, err := equalizer.New(config.EqualizerConfig{
		EqualizerMode: config.EqualizerModeMultiband,
		Bands:         3,
		LowHz:         100,
		HighHz:        10000,
		Gains:         "0,0,0",
	})
	if err != nil {
		t.Fatal(err)
	}
	values := []float32{0.2, -0.5, 0.8, -0.1, 0.05, 0.9}
	send(t, item, frame(48000, 0, values))
	assertSamplesTol(t, receive(t, item), values, 1e-4)
	if err := item.Flush(); err != nil {
		t.Fatal(err)
	}
	assertEOF(t, item)
}
