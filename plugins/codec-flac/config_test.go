package flac

import (
	"bytes"
	"log"
	"testing"
)

func TestPresetConfig(t *testing.T) {
	for level := 0; level <= 8; level++ {
		config := PresetConfig(level).ApplyDefaults()
		if err := config.Validate(); err != nil {
			t.Fatalf("level %d: %v", level, err)
		}
	}
	if got := PresetConfig(0).ApplyDefaults(); got.BlockSize != 1152 || got.StereoMode != 0 {
		t.Fatalf("level 0 = %#v", got)
	}
	if got := PresetConfig(8).ApplyDefaults(); got.MaxLPCOrder != 12 || len(got.Apodizations) != 6 {
		t.Fatalf("level 8 = %#v", got)
	}
}

func TestPresetConfigClampsWithWarning(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	if got := PresetConfig(-1).ApplyDefaults(); got.BlockSize != PresetConfig(0).ApplyDefaults().BlockSize {
		t.Fatal("negative level did not clamp to 0")
	}
	if got := PresetConfig(9).ApplyDefaults(); got.MaxLPCOrder != PresetConfig(8).ApplyDefaults().MaxLPCOrder {
		t.Fatal("large level did not clamp to 8")
	}
	if output.Len() == 0 {
		t.Fatal("expected warning")
	}
}

func TestWithPreset(t *testing.T) {
	config := NewEncoderConfig(WithPreset(8), WithBlockSize(2048)).ApplyDefaults()
	if config.BlockSize != 2048 {
		t.Fatalf("BlockSize = %d, want 2048", config.BlockSize)
	}
	if config.MaxLPCOrder != 12 {
		t.Fatalf("MaxLPCOrder = %d, want 12", config.MaxLPCOrder)
	}
}

func TestWithPresetReplacesPreviousOptions(t *testing.T) {
	config := NewEncoderConfig(WithBlockSize(2048), WithPreset(8)).ApplyDefaults()
	if config.BlockSize != 4096 {
		t.Fatalf("BlockSize = %d, want 4096", config.BlockSize)
	}
}
