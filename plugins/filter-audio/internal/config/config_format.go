package config

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	mediapcm "github.com/godexture/core/domain/media/pcm"
)

type FormatConfig struct {
	Format        media.SampleFormat `name:"format" help:"Target sample format"`
	BitsPerSample int                `name:"bits-per-sample" help:"Target effective bit depth"`
}

var DefaultFormatConfig = FormatConfig{}

func (c FormatConfig) EffectiveBitsPerSample() int {
	return media.EffectiveBitsPerSample(c.Format, c.BitsPerSample)
}

func (c FormatConfig) Validate() error {
	if err := mediapcm.ValidateFormat(c.Format); err != nil {
		return err
	}
	if c.BitsPerSample < 0 || c.BitsPerSample > c.Format.BitsPerSample() {
		return fmt.Errorf("invalid bits per sample: %d", c.BitsPerSample)
	}
	return nil
}
