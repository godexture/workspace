package flac

import (
	"bytes"
	"log"
	"testing"
)

func TestPresetConfig(t *testing.T) {
	t.Parallel()
	for level := 0; level <= 23; level++ {
		config := MustNewEncoderConfig(WithPreset(level))
		if err := config.Resolve().Validate(); err != nil {
			t.Fatalf("level %d: %v", level, err)
		}
	}
	if got := MustNewEncoderConfig(WithPreset(0)); got.MaxLPCOrder != 0 || got.MaxRicePartitionOrder != 0 || got.StereoMode != StereoIndependent {
		t.Fatalf("level 0 = %#v", got)
	}
	if got := MustNewEncoderConfig(WithPreset(12)); got.MaxLPCOrder != 10 || got.RiceCost != RiceCostEstimated || got.BlockSplitMode != BlockSplitEstimated {
		t.Fatalf("level 12 = %#v", got)
	}
	if got := MustNewEncoderConfig(WithPreset(16)); got.BlockSplitDepth != 2 || got.BlockSplitMode != BlockSplitExact || got.RiceCost != RiceCostExact {
		t.Fatalf("level 16 = %#v", got)
	}
	if got := MustNewEncoderConfig(WithPreset(23)); got.MaxLPCOrder != 20 || got.MaxRicePartitionOrder != 8 || len(got.Apodizations) != 21 ||
		got.BlockSplitDepth != 4 || got.BlockSplitMode != BlockSplitExact || got.RiceCost != RiceCostExact ||
		!got.EnablePrecisionSearch || got.FixedOrderSearch != OrderSearchEstimated || got.LPCOrderSearch != OrderSearchEstimated {
		t.Fatalf("level 23 = %#v", got)
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
			cfg := MustNewEncoderConfig()
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
	if got := MustNewEncoderConfig(WithPreset(-1)); got.MaxLPCOrder != MustNewEncoderConfig(WithPreset(0)).MaxLPCOrder {
		t.Fatal("negative level did not clamp to 0")
	}
	if got := MustNewEncoderConfig(WithPreset(24)); got.MaxLPCOrder != MustNewEncoderConfig(WithPreset(23)).MaxLPCOrder {
		t.Fatal("large level did not clamp to 23")
	}
	if output.Len() == 0 {
		t.Fatal("expected warning")
	}
}

func TestWithPreset(t *testing.T) {
	t.Parallel()
	config := MustNewEncoderConfig(WithPreset(23), WithBlockSize(2048))
	if config.BlockSize != 2048 {
		t.Fatalf("BlockSize = %d, want 2048", config.BlockSize)
	}
	if config.MaxLPCOrder != 20 {
		t.Fatalf("MaxLPCOrder = %d, want 20", config.MaxLPCOrder)
	}
}

func TestWithPresetReplacesPreviousOptions(t *testing.T) {
	t.Parallel()
	config := MustNewEncoderConfig(WithBlockSize(2048), WithPreset(23))
	if config.BlockSize != 4096 {
		t.Fatalf("BlockSize = %d, want 4096", config.BlockSize)
	}
}
