package equalizer

import (
	"math"
	"strings"
	"testing"

	"github.com/godexture/godec/plugin/audio/internal/config"
)

func TestResolveBandsAutoSplit(t *testing.T) {
	tests := []struct {
		name  string
		bands int
		wantQ float64
	}{
		{"ten bands", 10, 1.41926155886544},
		{"thirty-one bands", 31, 4.47843845769958},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.DefaultEqualizerConfig
			cfg.EqualizerMode = config.EqualizerModeMultiband
			cfg.Bands = test.bands
			cfg.Gains = strings.TrimSuffix(strings.Repeat("0,", test.bands), ",")
			bands, err := resolveBands(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if len(bands) != test.bands {
				t.Fatalf("len(bands) = %d, want %d", len(bands), test.bands)
			}
			for index, band := range bands {
				if index > 0 && !(bands[index-1].freq < band.freq) {
					t.Fatalf("bands are not ascending: %#v", bands)
				}
				assertNear(t, band.q, test.wantQ, 1e-9, "band Q")
			}
		})
	}
}

func TestResolveBandsManualOverrideSortsFrequencyGainPairs(t *testing.T) {
	cfg := config.DefaultEqualizerConfig
	cfg.EqualizerMode = config.EqualizerModeMultiband
	cfg.ManualBands = "1000,100,10000"
	cfg.Gains = "1,2,3"
	bands, err := resolveBands(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []pair{{100, 2}, {1000, 1}, {10000, 3}} {
		if bands[index].freq != want.freq || bands[index].gainDB != want.gain {
			t.Fatalf("band[%d] = %#v, want %#v", index, bands[index], want)
		}
	}
}

func TestResolveBandsSingleBand(t *testing.T) {
	auto := config.DefaultEqualizerConfig
	auto.EqualizerMode = config.EqualizerModeMultiband
	auto.Bands = 1
	auto.Gains = "0"
	autoBands, err := resolveBands(auto)
	if err != nil {
		t.Fatal(err)
	}
	assertNear(t, autoBands[0].freq, math.Sqrt(auto.LowHz*auto.HighHz), 1e-9, "auto frequency")
	assertNear(t, autoBands[0].q, math.Sqrt2, 1e-9, "auto Q")

	manual := auto
	manual.ManualBands = "440"
	manual.Gains = "3"
	manualBands, err := resolveBands(manual)
	if err != nil {
		t.Fatal(err)
	}
	assertNear(t, manualBands[0].freq, 440, 1e-9, "manual frequency")
	assertNear(t, manualBands[0].q, math.Sqrt2, 1e-9, "manual Q")
}

func TestResolveBandsSingleModePassesFieldsThrough(t *testing.T) {
	cfg := config.EqualizerConfig{EqualizerMode: config.EqualizerModeSingle, Type: config.EqualizerTypeHighPass, FrequencyHz: 400, GainDB: 2, Q: 0.5}
	bands, err := resolveBands(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(bands) != 1 || bands[0].eqType != cfg.Type || bands[0].freq != cfg.FrequencyHz || bands[0].gainDB != cfg.GainDB || bands[0].q != cfg.Q {
		t.Fatalf("bands = %#v", bands)
	}
}
