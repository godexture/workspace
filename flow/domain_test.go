package flow

import (
	"context"

	"github.com/godexture/godec/media/schema"
)

// testDomain is the failure domain a test holds for the slots it owns itself.
var testDomain Collector

// edge is the edge a test hands to code that needs one. It binds slots to
// testDomain under typ and accepts whatever is emitted.
type edge[T any] struct{ typ schema.Type[T] }

func (e edge[T]) Own(into *Item[T], value T) {
	into.Bind(e.typ, &testDomain)
	into.Set(value)
}

func (edge[T]) Emit(context.Context, *Item[T]) error { return nil }
