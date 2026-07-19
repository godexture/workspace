package config

type DecoderConfig struct {
	Strict bool
}

var DefaultDecoderConfig = DecoderConfig{
	Strict: false,
}
