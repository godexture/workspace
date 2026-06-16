package domain

type DecoderConfig struct{}

func (DecoderConfig) NodeConfigaration() {}

type EncoderConfig struct {
	Bitrate int
}

func (EncoderConfig) NodeConfigaration() {}
