// Package release drops groups of ownership cells on runtime cleanup paths.
//
// Only the runtime ever holds several cells at once: a queue ring, a fan-out's
// branches, a fan-in's batch. A component owns one cell at a time and releases
// it with defer, so grouping belongs here rather than on the public contract.
package release

import "github.com/godexture/godec/flow"

// All releases every slot in the group.
//
// It guarantees only that each one is tried. A slot that cannot release reports
// to its own failure domain, so one broken Drop neither raises nor strands the
// owners behind it, and this needs no error of its own.
func All[T any](slots []flow.Item[T]) {
	for index := range slots {
		slots[index].Drop()
	}
}
