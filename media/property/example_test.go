package property_test

import (
	"fmt"

	"github.com/godexture/godec/media/property"
)

type propertyExampleRateID struct{}

// A property key declares its canonical encoding, and Set updates return a
// new immutable value.
func ExampleSet() {
	rate := property.Define[propertyExampleRateID, int](property.Scalar[int]())
	original := property.New()
	updated, err := property.Put(original, rate, 48_000)
	if err != nil {
		panic(err)
	}
	value, _ := rate.Get(updated)
	canonical, _ := rate.Canonical(value)

	fmt.Println(original.Len(), updated.Len())
	fmt.Println(value, string(canonical))
	// Output:
	// 0 1
	// 48000 int:48000
}
