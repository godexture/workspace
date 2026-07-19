package domain

type DecoderConfig struct{}

var DefaultDecoderConfig = DecoderConfig{}

type EncoderConfig struct {
	Bitrate int
}

var DefaultEncoderConfig = EncoderConfig{Bitrate: 128000}
