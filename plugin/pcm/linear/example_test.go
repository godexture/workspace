package linear_test

import (
	"fmt"

	"github.com/godexture/godec/plugin/pcm/linear"
)

// Set composes the raw format's components and declarations without package
// initialization or a process-global registry.
func ExampleSet() {
	set := linear.Set()
	fmt.Println(len(set.Components()))
	fmt.Println(linear.Raw().Alternatives()[0].Capabilities[0])
	// Output:
	// 5
	// sequential-read
}
