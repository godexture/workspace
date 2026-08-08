package flow_test

import (
	"fmt"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/schema"
)

type flowExampleUnitID struct{}

// A shape declares typed ports without exposing queues or scheduling.
func ExampleNewShape() {
	units := schema.Define[flowExampleUnitID, int](schema.Traits[int]{})
	shape := flow.NewShape(
		[]flow.Port{flow.In("input", units)},
		[]flow.Port{flow.Out("output", units, flow.Optional(), flow.Many())},
	)

	fmt.Println(shape.Validate())
	fmt.Println(shape.Inputs[0].Schema().Identity().Name())
	fmt.Println(shape.Outputs[0].Required(), shape.Outputs[0].Multiplicity())
	// Output:
	// <nil>
	// flowExampleUnitID
	// false 2
}

// Share retains a value without consuming the borrowed input; Take transfers
// its existing ownership instead.
func ExampleInput_Share() {
	retains := 0
	releases := 0
	input := flow.NewInputWithTraits(7, func(value int) int {
		retains++
		return value
	}, func(int) {
		releases++
	})

	shared := input.Share()
	owned := input.Take()
	shared.Release()
	owned.Release()
	fmt.Println(retains, releases)
	// Output: 1 2
}
