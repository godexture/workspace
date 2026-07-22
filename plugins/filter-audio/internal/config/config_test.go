package config

import (
	"testing"

	"github.com/godexture/core/domain/media"
)

func TestFormatConfigEffectiveBitsPerSample(t *testing.T) {
	t.Parallel()
	if got, want := (FormatConfig{Format: media.SampleFormatS24}).EffectiveBitsPerSample(), 24; got != want {
		t.Fatalf("default effective bits = %d, want %d", got, want)
	}
	if got, want := (FormatConfig{Format: media.SampleFormatS32, BitsPerSample: 20}).EffectiveBitsPerSample(), 20; got != want {
		t.Fatalf("explicit effective bits = %d, want %d", got, want)
	}
}

func TestSpeedConfigValidate(t *testing.T) {
	t.Parallel()
	if err := (SpeedConfig{Factor: 0, Mode: SpeedModeInterpolate}).Validate(); err == nil {
		t.Fatal("want error for non-positive factor")
	}
	if err := (SpeedConfig{Factor: 2, Mode: SpeedModeInterpolate}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (SpeedConfig{Factor: 2, Mode: SpeedModeRelabel}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (SpeedConfig{Factor: 2, Mode: "bogus"}).Validate(); err == nil {
		t.Fatal("want error for invalid mode")
	}
}
