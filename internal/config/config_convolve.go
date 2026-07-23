package config

import "fmt"

// ConvolutionConfig configures a uniform partitioned FFT convolution
// filter. ImpulseResponse and ImpulseRate carry raw sample data rather
// than CLI-representable primitives, so they intentionally have no `name`
// tag: they are set only through the Go API (filter.WithImpulseResponse /
// filter.WithImpulseRate), never from a CLI flag string.
//
// A single impulse response channel applies to every input channel
// (mono-apply broadcast); an impulse response with one channel per input
// channel convolves each input channel with its matching channel
// independently. Cross-channel ("true stereo") convolution is out of
// scope.
type ConvolutionConfig struct {
	ImpulseResponse [][]float32
	ImpulseRate     int

	WetDryMix float64 `name:"wet-dry-mix" help:"Dry/wet balance: 0 = unprocessed input, 1 = fully convolved"`
	Normalize bool    `name:"normalize" help:"Scale the impulse response down if its gain could exceed unity, to avoid clipping"`
	BlockSize int     `name:"block-size" help:"FFT hop size in samples, rounded up to a power of two; 0 selects a default"`
}

var DefaultConvolutionConfig = ConvolutionConfig{WetDryMix: 1, Normalize: true}

func (c ConvolutionConfig) Validate() error {
	if len(c.ImpulseResponse) == 0 {
		return fmt.Errorf("convolution impulse response must have at least one channel")
	}
	length := -1
	for i, channel := range c.ImpulseResponse {
		if len(channel) == 0 {
			return fmt.Errorf("convolution impulse response channel %d is empty", i)
		}
		if length == -1 {
			length = len(channel)
		} else if len(channel) != length {
			return fmt.Errorf("convolution impulse response channels have mismatched lengths")
		}
	}
	if c.ImpulseRate < 0 {
		return fmt.Errorf("convolution impulse response rate must not be negative")
	}
	if !finite(c.WetDryMix) || c.WetDryMix < 0 || c.WetDryMix > 1 {
		return fmt.Errorf("convolution wet/dry mix must be within [0, 1]")
	}
	if c.BlockSize < 0 {
		return fmt.Errorf("convolution block size must not be negative")
	}
	return nil
}
