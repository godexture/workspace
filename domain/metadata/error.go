package metadata

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("metadata: key not found")

type TypeError struct {
	Key      any
	Expected string
	Actual   string
}

func (e *TypeError) Error() string {
	return fmt.Sprintf("metadata: type mismatch for key %v: expected %s, got %s", e.Key, e.Expected, e.Actual)
}
