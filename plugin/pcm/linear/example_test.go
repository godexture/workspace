package linear_test

import (
	"fmt"

	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin/pcm/linear"
)

// Set composes the raw format's components and declarations without package
// initialization or a process-global registry.
func ExampleSet() {
	set := linear.Set()
	fmt.Println(len(set.Components()))
	for _, component := range set.Components() {
		if trait, ok := format.ReadOf(component); ok {
			fmt.Println(trait.Requirements().Alternatives[0].Capabilities[0])
		}
	}
	// Output:
	// 5
	// sequential-read
}
