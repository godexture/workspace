package schema_test

import (
	"fmt"

	"github.com/godexture/godec/media/schema"
)

type schemaExampleUnitID struct{}
type schemaExampleUnit struct{ Bytes int }

// Define keeps typed traits on the schema while its descriptor can cross an
// erased component boundary and construct a typed product once at open time.
func ExampleDefine() {
	units := schema.Define[schemaExampleUnitID, schemaExampleUnit](schema.Traits[schemaExampleUnit]{
		Size: func(value schemaExampleUnit) int { return value.Bytes },
	})
	product, err := units.Descriptor().NewPipe()
	if err != nil {
		panic(err)
	}
	queue := product.(schema.Queue[schemaExampleUnit])
	queue.Push(schemaExampleUnit{Bytes: 12})
	value, _ := queue.Pop()
	size, _ := units.Size(value)

	fmt.Println(units.Identity().Name(), size)
	// Output: schemaExampleUnitID 12
}
