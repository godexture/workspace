// Package release drops groups of ownership cells on runtime cleanup paths.
//
// Only the runtime ever holds several cells at once: a queue ring, a fan-out's
// branches, a fan-in's batch. A component owns one cell at a time and releases
// it with defer, so grouping belongs here rather than on the public contract.
package release

import (
	"errors"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
)

// All releases every cell in order and keeps going when one of them panics.
//
// A declared Drop is third-party code. Releasing a group of owners one panic
// at a time would strand every owner after the first failure, which is exactly
// what cleanup exists to prevent, so each release is isolated and the failures
// are reported together afterwards. It never panics: cleanup runs on paths
// that have no recovery boundary left.
func All[T any](cells []flow.Item[T]) error {
	var problems []error
	for index := range cells {
		if err := one(&cells[index]); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

func one[T any](cell *flow.Item[T]) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.New("flow item release panicked: " + diagnostic.Recovered(recovered))
		}
	}()
	cell.Drop()
	return nil
}
