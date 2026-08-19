package format

import (
	"errors"
	"sort"

	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

// ErrInvalidSelection reports an invalid exact stream selection.
var ErrInvalidSelection = errors.New("format stream selection is invalid")

type selectionTraitKey struct{}

var selectionKey = plugin.TraitKeyOf[selectionTraitKey]()

// Selection is an immutable exact stream set prepared for one Format. Its
// order is canonical; an absent Selection means that the Format should retain
// every eligible stream.
type Selection struct {
	format  Format
	streams []stream.ID
}

// NewSelection constructs the canonical non-empty stream set for value.
func NewSelection(value Format, ids ...stream.ID) (Selection, error) {
	if !value.Valid() || len(ids) == 0 {
		return Selection{}, ErrInvalidSelection
	}
	streams := append([]stream.ID(nil), ids...)
	sort.Slice(streams, func(left, right int) bool { return streams[left].String() < streams[right].String() })
	for index, id := range streams {
		if id.IsZero() || index > 0 && streams[index-1] == id {
			return Selection{}, ErrInvalidSelection
		}
	}
	return Selection{format: value, streams: streams}, nil
}

func (s Selection) Valid() bool {
	if !s.format.Valid() || len(s.streams) == 0 {
		return false
	}
	for index, id := range s.streams {
		if id.IsZero() || index > 0 && s.streams[index-1].String() >= id.String() {
			return false
		}
	}
	return true
}

func (s Selection) Format() Format       { return s.format }
func (s Selection) Streams() []stream.ID { return append([]stream.ID(nil), s.streams...) }

// WithSelection attaches one Format-bound exact stream set to an immutable
// component CompileContext. A missing selection remains the preserve-all
// behavior and is intentionally different from an empty selection.
func WithSelection(ctx plugin.CompileContext, value Selection) (plugin.CompileContext, error) {
	if !value.Valid() {
		return ctx, ErrInvalidSelection
	}
	return plugin.CompileContextWithTrait(ctx, selectionKey, value)
}

// SelectionOf recovers the exact stream set prepared for expected. A false
// result means no explicit selection was supplied and the Format should use
// its preserve-all behavior.
func SelectionOf(ctx plugin.CompileContext, expected Format) (Selection, bool) {
	value, ok := plugin.TraitValueOf[Selection](ctx, selectionKey)
	if !ok || !value.Valid() || !expected.Valid() || value.format.Identity() != expected.Identity() {
		return Selection{}, false
	}
	return value, true
}
