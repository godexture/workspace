package format

import (
	"context"
	"errors"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/plugin"
)

var (
	ErrInspectUnavailable = errors.New("format does not provide Inspect")
	ErrInvalidInspection  = errors.New("format inspection is invalid")
)

type inspectionTraitKey struct{}

var inspectionKey = plugin.TraitKeyOf[inspectionTraitKey]()

// InspectContext is the bounded input view for inspecting one selected
// Format before component compilation.
type InspectContext struct {
	context context.Context
	opening access.Opening
}

func NewInspectContext(ctx context.Context, opening access.Opening) InspectContext {
	if ctx == nil {
		ctx = context.Background()
	}
	return InspectContext{context: ctx, opening: opening}
}

func (c InspectContext) Context() context.Context {
	if c.context == nil {
		return context.Background()
	}
	return c.context
}

func (c InspectContext) Opening() access.Opening { return c.opening }

func (c InspectContext) Valid() bool {
	return c.opening.Valid() && c.opening.Direction() == access.SourceDirection
}

// Inspection is the erased transport for one Format-owned immutable value.
// Construction and recovery stay typed at the Format package boundary.
type Inspection struct {
	format Format
	value  any
}

func NewInspection[T any](value Format, prepared T) Inspection {
	return Inspection{format: value, value: prepared}
}

func (i Inspection) Format() Format { return i.format }
func (i Inspection) Valid() bool    { return i.format.Valid() && i.value != nil }

// WithInspection attaches one Format inspection to an immutable component
// CompileContext using the shared plugin trait store.
func WithInspection(ctx plugin.CompileContext, value Inspection) (plugin.CompileContext, error) {
	if !value.Valid() {
		return ctx, ErrInvalidInspection
	}
	return plugin.CompileContextWithTrait(ctx, inspectionKey, value)
}

// InspectionOf returns the typed inspection prepared for expected.
func InspectionOf[T any](ctx plugin.CompileContext, expected Format) (T, bool) {
	var zero T
	value, ok := plugin.TraitValueOf[Inspection](ctx, inspectionKey)
	if !ok || !value.Valid() || !expected.Valid() || value.format.Identity() != expected.Identity() {
		return zero, false
	}
	prepared, ok := value.value.(T)
	if !ok {
		return zero, false
	}
	return prepared, true
}
