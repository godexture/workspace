package format

import (
	"github.com/godexture/godec/access"
	"github.com/godexture/godec/plugin"
)

// SourceOpening returns the read-only source view that backs this Format's
// prepared inspection. The Host owns the view and keeps it valid only from
// Open until the operator is closed.
func SourceOpening(ctx plugin.OpenContext) (access.Opening, bool) {
	opening, ok := plugin.Source[access.Opening](ctx)
	return opening, ok && opening.Valid() && opening.Direction() == access.SourceDirection
}
