package stream_test

import (
	"fmt"

	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
)

type streamExampleUnitID struct{}
type streamExampleRateID struct{}

// A descriptor keeps a stream identity, open unit schema, integer time base,
// and immutable properties without a closed media-kind enum.
func ExampleNewDescriptor() {
	units := schema.Define[streamExampleUnitID, int](schema.Traits[int]{})
	rate := property.Define[streamExampleRateID, int](property.Scalar[int]())
	properties, _ := property.Put(property.New(), rate, 48_000)
	descriptor, err := stream.NewDescriptor("audio-1", units.Identity(), timing.MustBase(1, 48_000), properties)
	if err != nil {
		panic(err)
	}
	value, _ := rate.Get(descriptor.Properties())

	fmt.Println(descriptor.ID(), descriptor.Schema().Name())
	fmt.Println(descriptor.TimeBase(), value)
	// Output:
	// audio-1 streamExampleUnitID
	// 1/48000 48000
}
