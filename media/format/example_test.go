package format_test

import (
	"fmt"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/format"
)

type formatExampleID struct{}
type formatExampleMetadataID struct{}

// A format declares direction-neutral identity and physical carriers without
// selecting a provider, codec, or metadata implementation.
func ExampleDefine() {
	metadataCarrier := carrier.Define[formatExampleMetadataID]()
	value, err := format.Define[formatExampleID]([]carrier.ID{metadataCarrier})
	if err != nil {
		panic(err)
	}

	fmt.Println(value.Identity().Name(), len(value.Carriers()))
	fmt.Println(value.Carriers()[0].Name(), value.Packetized())
	// Output:
	// formatExampleID 1
	// formatExampleMetadataID false
}
