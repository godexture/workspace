package buffer_test

import (
	"fmt"

	"github.com/godexture/godec/media/buffer"
)

// An Allocator creates bounded aligned backing allocations for a Job-local
// owner. A shared handle retains an allocation independently of its first owner.
func ExampleAllocator_Allocate() {
	allocator, err := buffer.NewAllocator(1024)
	if err != nil {
		panic(err)
	}
	handle, err := allocator.Allocate(buffer.Spec{
		Alignment: 16,
		Planes: []buffer.PlaneSpec{
			{Size: 4, Padding: 2},
			{Size: 3},
		},
	})
	if err != nil {
		panic(err)
	}
	edit, err := handle.Edit(nil)
	if err != nil {
		panic(err)
	}
	bytes, _ := edit.MutableBytes()
	copy(bytes, []byte{1, 2, 3, 4})
	handle = edit.Handle()
	if err := edit.Commit(); err != nil {
		panic(err)
	}
	shared := handle.Share()
	handle.Release()
	defer shared.Release()

	first, _ := shared.Plane(0)
	fmt.Println(shared.Valid(), shared.Layout().Alignment)
	fmt.Println(first.AppendTo(nil))
	// Output:
	// true 16
	// [1 2 3 4]
}
