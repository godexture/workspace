package key_test

import (
	"fmt"

	"github.com/godexture/godec/media/key"
)

type keyExamplePayloadID struct{}

// Reference-valued keys declare their snapshot rule once. The erased view
// carries that rule to every container using the key.
func ExampleDefine() {
	payload := key.Define[keyExamplePayloadID, []byte](func(value []byte) []byte {
		return append([]byte(nil), value...)
	})
	source := []byte{1, 2, 3}
	cloned, ok := payload.Erased().Clone(source)
	source[0] = 9

	fmt.Println(payload.ID().Name(), ok, cloned.([]byte))
	// Output: keyExamplePayloadID true [1 2 3]
}
