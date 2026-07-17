package flac

import (
	"bytes"
	"log"
	"testing"

	"github.com/godexture/sdk/optional"
)

func TestPresetConfig(t *testing.T) {
	t.Parallel()
	for level := 0; level <= 8; level++ {
		config := PresetConfig(level).ApplyDefaults()
		if err := config.Validate(); err != nil {
			t.Fatalf("level %d: %v", level, err)
		}
	}
	if got := PresetConfig(0).ApplyDefaults(); got.BlockSize != 1152 || got.StereoMode != 0 {
		t.Fatalf("level 0 = %#v", got)
	}
	if got := PresetConfig(7).ApplyDefaults(); got.BlockSplitDepth != 2 || got.BlockSplitMode != BlockSplitEstimated {
		t.Fatalf("level 7 = %#v", got)
	}
	if got := PresetConfig(8).ApplyDefaults(); got.MaxLPCOrder != 12 || len(got.Apodizations) != 6 || got.BlockSplitDepth != 2 || got.BlockSplitMode != BlockSplitExact {
		t.Fatalf("level 8 = %#v", got)
	}
}

func TestBlockSplitConfigValidation(t *testing.T) {
	t.Parallel()
	for _, config := range []struct {
		name string
		edit func(*EncoderConfig)
	}{
		{"negative depth", func(c *EncoderConfig) { c.BlockSplitDepth = optional.Some(-1) }},
		{"small leaf", func(c *EncoderConfig) { c.BlockSplitDepth = optional.Some(9) }},
		{"uneven split", func(c *EncoderConfig) { c.BlockSize, c.BlockSplitDepth = optional.Some(4095), optional.Some(2) }},
		{"invalid mode", func(c *EncoderConfig) { c.BlockSplitMode = optional.Some(BlockSplitMode(2)) }},
	} {
		t.Run(config.name, func(t *testing.T) {
			cfg := NewEncoderConfig()
			config.edit(&cfg)
			if err := cfg.ApplyDefaults().Validate(); err == nil {
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
	t.Parallel()
	config := NewEncoderConfig(WithPreset(8), WithBlockSize(2048)).ApplyDefaults()
	if config.BlockSize != 2048 {
		t.Fatalf("BlockSize = %d, want 2048", config.BlockSize)
	}
	if config.MaxLPCOrder != 12 {
		t.Fatalf("MaxLPCOrder = %d, want 12", config.MaxLPCOrder)
	}
}

func TestWithPresetReplacesPreviousOptions(t *testing.T) {
	t.Parallel()
	config := NewEncoderConfig(WithBlockSize(2048), WithPreset(8)).ApplyDefaults()
	if config.BlockSize != 4096 {
		t.Fatalf("BlockSize = %d, want 4096", config.BlockSize)
	}
}
