package config

type DecoderConfig struct {
	Strict bool `name:"strict" help:"Enable strict decoding mode"`
}

var DefaultDecoderConfig = DecoderConfig{
	Strict: false,
}
