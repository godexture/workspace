package format_test

import (
	"fmt"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/format"
)

type formatExampleID struct{}
type formatExampleMetadataID struct{}

// A format declares alternative byte-access requirements and physical
// carriers without selecting a provider, codec, or metadata implementation.
func ExampleDefine() {
	metadataCarrier := carrier.Define[formatExampleMetadataID]()
	value, err := format.Define[formatExampleID](
		[]access.Alternative{
			access.AnyOf(access.SequentialRead),
			access.AnyOf(access.RandomRead, access.StableSize),
		},
		[]carrier.ID{metadataCarrier},
	)
	if err != nil {
		panic(err)
	}

	fmt.Println(value.Identity().Name(), len(value.Alternatives()))
	fmt.Println(value.Carriers()[0].Name(), value.Packetized())
	// Output:
	// formatExampleID 2
	// formatExampleMetadataID false
}
