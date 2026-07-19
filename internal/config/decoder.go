package config

import "runtime"

type DecoderConfig struct {
	Strict  bool
	Workers int
}

var DefaultDecoderConfig = DecoderConfig{
	Strict:  false,
	Workers: runtime.GOMAXPROCS(0),
}
