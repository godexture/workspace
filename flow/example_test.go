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
func ExampleItem_Fork() {
	retains := 0
	releases := 0
	item := flow.NewItemWithTraits(7, func(value int) int {
		retains++
		return value
	}, func(int) {
		releases++
	})
	defer item.Drop()

	var branch flow.Item[int]
	item.Fork(&branch)
	branch.Drop()
	fmt.Println(retains, releases)
	// Output: 1 1
}
