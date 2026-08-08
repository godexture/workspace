package carrier_test

import (
	"fmt"

	"github.com/godexture/godec/media/carrier"
)

type carrierExampleArtworkID struct{}

// Carrier identities come from marker types, so independent plugins do not
// coordinate handwritten global names.
func ExampleDefine() {
	artwork := carrier.Define[carrierExampleArtworkID]()
	fmt.Println(artwork.Valid(), artwork.Name())
	// Output: true carrierExampleArtworkID
}
