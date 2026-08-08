package schema_test

import (
	"fmt"

	"github.com/godexture/godec/media/schema"
)

type schemaExampleUnitID struct{}
type schemaExampleUnit struct{ Bytes int }

// Define keeps data-path traits on the typed schema while its descriptor can
// cross planner boundaries without exposing those operations through any.
func ExampleDefine() {
	units := schema.Define[schemaExampleUnitID, schemaExampleUnit](schema.Traits[schemaExampleUnit]{
		Size: func(value schemaExampleUnit) int { return value.Bytes },
	})
	value := schemaExampleUnit{Bytes: 12}
	size, _ := units.Size(value)

	fmt.Println(units.Identity().Name(), units.Descriptor().Payload().Name(), size)
	// Output: schemaExampleUnitID schemaExampleUnit 12
}
