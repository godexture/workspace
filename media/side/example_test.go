package side_test

import (
	"fmt"

	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/side"
)

type sideExampleNoteID struct{}

// Side data is immutable, ordered, and permits repeated values for one open
// key.
func ExampleAdd() {
	note := key.Define[sideExampleNoteID, string]()
	original := side.Data{}
	first, _ := side.Add(original, note, "first")
	second, _ := side.Add(first, note, "second")

	fmt.Println(original.Len(), second.Len())
	fmt.Println(side.Values(second, note))
	// Output:
	// 0 2
	// [first second]
}
