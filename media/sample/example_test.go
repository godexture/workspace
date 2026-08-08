package sample_test

import (
	"fmt"

	"github.com/godexture/godec/media/sample"
)

// A Description moves stream-invariant sample information into canonical
// properties while the frame schema carries the scalar Go type.
func ExampleDescription() {
	description := sample.Description{
		Format:    sample.S16Interleaved,
		ValidBits: 16,
		Rate:      48_000,
		Layout:    sample.Stereo,
		Endian:    sample.LittleEndian,
	}
	properties, _ := description.Properties()
	decoded, _ := sample.FromProperties(properties)

	fmt.Println(decoded.Format, decoded.Rate, decoded.Layout.Channels())
	fmt.Println(sample.S16().Identity() != sample.F32().Identity())
	// Output:
	// s16 48000 2
	// true
}
