package domain

type DecoderConfig struct{}

func (DecoderConfig) NodeConfiguration() {}

type EncoderConfig struct {
	Bitrate int
}

func (EncoderConfig) NodeConfiguration() {}

var DefaultEncoderConfig = EncoderConfig{Bitrate: 128000}
