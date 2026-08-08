package buffer_test

import (
	"fmt"

	"github.com/godexture/godec/media/buffer"
)

// Allocate creates one aligned backing allocation for all declared planes.
// A shared handle retains that allocation independently of the first owner.
func ExampleAllocate() {
	handle, err := buffer.Allocate(buffer.Spec{
		Alignment: 16,
		Planes: []buffer.PlaneSpec{
			{Size: 4, Padding: 2},
			{Size: 3},
		},
	})
	if err != nil {
		panic(err)
	}
	bytes, _ := handle.MutableBytes()
	copy(bytes, []byte{1, 2, 3, 4})
	shared := handle.Share()
	handle.Release()
	defer shared.Release()

	first, _ := shared.Plane(0)
	fmt.Println(shared.Valid(), shared.Layout().Alignment)
	fmt.Println(first)
	// Output:
	// true 16
	// [1 2 3 4]
}
