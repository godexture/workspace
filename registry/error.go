package registry

import (
	"fmt"

	"github.com/godexture/core/domain/manifest"
)

type Error struct {
	Role manifest.NodeType
	Name string
}

func (e *Error) Error() string {
	return fmt.Sprintf("requested %s '%s' is not registered", e.Role, e.Name)
}
