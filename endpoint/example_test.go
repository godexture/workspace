package endpoint_test

import (
	"fmt"

	"github.com/godexture/godec/endpoint"
)

// Traits describe endpoint behavior without scanning or opening a device.
func ExampleNewTrait() {
	trait, err := endpoint.NewTrait(endpoint.LiveDynamic, endpoint.Realtime)
	if err != nil {
		panic(err)
	}

	fmt.Println(trait.Valid())
	fmt.Println(trait.Topology() == endpoint.LiveDynamic, trait.Mode() == endpoint.Realtime)
	// Output:
	// true
	// true true
}
