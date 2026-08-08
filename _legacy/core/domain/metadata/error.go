package metadata

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("metadata: key not found")

type TypeError struct {
	Expected string
	Actual   string
}

func (e *TypeError) Error() string {
	return fmt.Sprintf("metadata: type mismatch: expected %s, got %s", e.Expected, e.Actual)
}
