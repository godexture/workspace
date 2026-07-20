package flac

import (
	"bytes"
	"log"
	"testing"
)

func TestPresetConfig(t *testing.T) {
	t.Parallel()
	for level := 0; level <= 8; level++ {
		config := NewEncoderConfig(WithPreset(level))
		if err := config.Resolve().Validate(); err != nil {
			t.Fatalf("level %d: %v", level, err)
		}
	}
	if got := NewEncoderConfig(WithPreset(0)); got.BlockSize != 1152 || got.StereoMode != StereoIndependent {
		t.Fatalf("level 0 = %#v", got)
	}
	if got := NewEncoderConfig(WithPreset(7)); got.BlockSplitDepth != 2 || got.BlockSplitMode != BlockSplitEstimated {
		t.Fatalf("level 7 = %#v", got)
	}
	if got := NewEncoderConfig(WithPreset(8)); got.MaxLPCOrder != 12 || len(got.Apodizations) != 6 || got.BlockSplitDepth != 2 || got.BlockSplitMode != BlockSplitExact {
		t.Fatalf("level 8 = %#v", got)
	}
}

func TestBlockSplitConfigValidation(t *testing.T) {
	t.Parallel()
	for _, config := range []struct {
		name string
		edit func(*EncoderConfig)
	}{
		{"negative depth", func(c *EncoderConfig) { c.BlockSplitDepth = -1 }},
		{"small leaf", func(c *EncoderConfig) { c.BlockSplitDepth = 9 }},
		{"uneven split", func(c *EncoderConfig) { c.BlockSize, c.BlockSplitDepth = 4095, 2 }},
		{"invalid mode", func(c *EncoderConfig) { c.BlockSplitMode = BlockSplitMode("invalid") }},
	} {
		t.Run(config.name, func(t *testing.T) {
			cfg := NewEncoderConfig()
			config.edit(&cfg)
			if err := cfg.Resolve().Validate(); err == nil {
				t.Fatal("Validate() succeeded")
			}
		})
	}
}

func TestPresetConfigClampsWithWarning(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	if got := NewEncoderConfig(WithPreset(-1)); got.BlockSize != NewEncoderConfig(WithPreset(0)).BlockSize {
		t.Fatal("negative level did not clamp to 0")
	}
	if got := NewEncoderConfig(WithPreset(9)); got.MaxLPCOrder != NewEncoderConfig(WithPreset(8)).MaxLPCOrder {
		t.Fatal("large level did not clamp to 8")
	}
	if output.Len() == 0 {
		t.Fatal("expected warning")
	}
}

func TestWithPreset(t *testing.T) {
	t.Parallel()
	config := NewEncoderConfig(WithPreset(8), WithBlockSize(2048))
	if config.BlockSize != 2048 {
		t.Fatalf("BlockSize = %d, want 2048", config.BlockSize)
	}
	if config.MaxLPCOrder != 12 {
		t.Fatalf("MaxLPCOrder = %d, want 12", config.MaxLPCOrder)
	}
}

func TestWithPresetReplacesPreviousOptions(t *testing.T) {
	t.Parallel()
	config := NewEncoderConfig(WithBlockSize(2048), WithPreset(8))
	if config.BlockSize != 4096 {
		t.Fatalf("BlockSize = %d, want 4096", config.BlockSize)
	}
}
