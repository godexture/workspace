package format

import (
	"context"
	"errors"
	"math"
	"reflect"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
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
	context  context.Context
	opening  access.Opening
	prepared plugin.CompileContext
	limit    resource.Bytes
	memory   resource.Bytes
}

func NewInspectContext(ctx context.Context, opening access.Opening, prepared plugin.CompileContext, limit, memory resource.Bytes) InspectContext {
	if ctx == nil {
		ctx = context.Background()
	}
	return InspectContext{context: ctx, opening: opening, prepared: prepared, limit: limit, memory: memory}
}

// Limit reports the bytes this Inspect may still read. Host enforces it on the
// opening as well; a Format consults it to refuse an allocation that a
// declared header size would otherwise dictate.
func (c InspectContext) Limit() resource.Bytes { return c.limit }

// MemoryLimit reports the bytes a Format may retain in its immutable
// inspection model. A source-aware format may also use it for a bounded
// rewrite workspace. It is separate from Limit, which accounts source reads.
func (c InspectContext) MemoryLimit() resource.Bytes { return c.memory }

func (c InspectContext) Context() context.Context {
	if c.context == nil {
		return context.Background()
	}
	return c.context
}

func (c InspectContext) Opening() access.Opening { return c.opening }

// Prepared returns the node-local marker-keyed values selected before Inspect.
// Typed packages such as metadata remain the only supported accessors.
func (c InspectContext) Prepared() plugin.CompileContext { return c.prepared }

func (c InspectContext) Valid() bool {
	return c.opening.Valid() && c.opening.Direction() == access.SourceDirection &&
		c.limit > 0 && uint64(c.limit) <= math.MaxInt64 &&
		c.memory > 0 && uint64(c.memory) <= math.MaxInt64
}

// Inspection is the erased transport for one Format-owned immutable value.
// Construction and recovery stay typed at the Format package boundary.
type Inspection struct {
	format Format
	value  any
}

// NewInspection transports one Format-owned immutable value unchanged.
// prepared may contain references, but its contents must remain frozen for
// every Compile that receives this inspection.
func NewInspection[T any](value Format, prepared T) Inspection {
	if !value.Valid() || isNilValue(prepared) {
		return Inspection{}
	}
	return Inspection{format: value, value: prepared}
}

func isNilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return reflected.IsNil()
	default:
		return false
	}
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
