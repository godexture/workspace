package flow_test

import (
	"fmt"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/schema"
)

type flowExampleUnitID struct{}
type exampleForkID struct{}

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

// Fork is the only way to obtain a second owner of one payload. The branch
// declares the domain it will be released in first: a slot that declares
// nothing cannot take ownership, so no payload ends up somewhere with no
// release and nowhere to report one that fails.
func ExampleItem_Fork() {
	retains := 0
	releases := 0
	var domain flow.Collector
	typ := schema.Define[exampleForkID](schema.Traits[int]{
		Fork: func(value int) int {
			retains++
			return value
		},
		Drop: func(int) { releases++ },
	})
	item := flow.NewItem(7, typ, &domain)
	defer item.Drop()

	var branch flow.Item[int]
	branch.Bind(typ, &domain)
	item.Fork(&branch)
	branch.Drop()
	fmt.Println(retains, releases)
	// Output: 1 1
}
