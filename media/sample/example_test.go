package sample_test

import (
	"fmt"

	"github.com/godexture/godec/media/sample"
)

// A Description moves stream-invariant sample information into canonical
// properties. Scalar coding, packing and byte order are independent, so a new
// wire representation adds one value to one axis instead of a combination.
func ExampleDescription() {
	wire := sample.Description{
		Coding:    sample.S24,
		Packing:   sample.Interleaved,
		Endian:    sample.LittleEndian,
		Rate:      48_000,
		Layout:    sample.Stereo(),
		ValidBits: 24,
	}
	properties, _ := wire.Properties()
	decoded, _ := sample.FromProperties(properties)

	fmt.Println(decoded.Coding, decoded.Rate, decoded.Layout, decoded.Layout.Count())
	fmt.Println(decoded.Decoded().Coding, decoded.Decoded().Packing)
	// Output:
	// s24 48000 FL+FR 2
	// s32 planar
}

// Positions that no vocabulary names still describe a stream: the layout keeps
// the channel count without inventing speakers for it.
func ExampleChannels() {
	fmt.Println(sample.Channels(3), sample.Channels(3).Positioned())
	fmt.Println(sample.Positions(sample.FrontLeft, sample.FrontRight, sample.LowFrequency))
	// Output:
	// 3ch false
	// FL+FR+LFE
}
