package internal

type DecoderConfig struct{}

func (DecoderConfig) NodeConfiguration() {}

var DefaultDecoderConfig = DecoderConfig{}
