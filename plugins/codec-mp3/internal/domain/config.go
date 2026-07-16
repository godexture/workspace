package domain

type DecoderConfig struct{}

type EncoderConfig struct {
	Bitrate int
}

var DefaultEncoderConfig = EncoderConfig{Bitrate: 128000}
