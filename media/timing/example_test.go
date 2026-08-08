package timing_test

import (
	"fmt"

	"github.com/godexture/godec/media/timing"
)

// Rescale uses explicit integer time bases and rounding policy.
func ExampleBase_Rescale() {
	from := timing.MustBase(1, 48_000)
	to := timing.MustBase(1, 1_000)
	value, err := from.Rescale(48_024, to, timing.RoundNearestEven)
	if err != nil {
		panic(err)
	}

	fmt.Println(value)
	// Output: 1000
}
