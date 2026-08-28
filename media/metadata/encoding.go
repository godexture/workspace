package metadata

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plugin"
)

var (
	ErrInvalidEncoding = errors.New("metadata encoding is invalid")
	ErrInvalidContext  = errors.New("metadata encoding context is invalid")
)

type encodingTraitKey struct{}

var encodingKey = plugin.TraitKeyOf[encodingTraitKey]()

// ParseContext is the immutable control-plane input for one carrier block.
type ParseContext struct {
	context  context.Context
	carrier  carrier.ID
	block    BlockID
	scope    Scope
	encoding plugin.Identity
	payload  Blob
}

func (c ParseContext) Context() context.Context {
	if c.context == nil {
		return context.Background()
	}
	return c.context
}

func (c ParseContext) Carrier() carrier.ID       { return c.carrier }
func (c ParseContext) Block() BlockID            { return c.block }
func (c ParseContext) Scope() Scope              { return c.scope }
func (c ParseContext) Encoding() plugin.Identity { return c.encoding }
func (c ParseContext) Payload() Blob             { return c.payload }
func (c ParseContext) valid() bool {
	return c.carrier.Valid() && c.block != "" && c.scope.Valid() && !c.encoding.IsZero() && c.payload.Valid()
}

// MarshalContext is the immutable control-plane input for one carrier block.
type MarshalContext struct {
	context  context.Context
	carrier  carrier.ID
	block    BlockID
	encoding plugin.Identity
	document Document
}

func (c MarshalContext) Context() context.Context {
	if c.context == nil {
		return context.Background()
	}
	return c.context
}

func (c MarshalContext) Carrier() carrier.ID       { return c.carrier }
func (c MarshalContext) Block() BlockID            { return c.block }
func (c MarshalContext) Encoding() plugin.Identity { return c.encoding }
func (c MarshalContext) Document() Document        { return c.document }
func (c MarshalContext) valid() bool {
	return c.carrier.Valid() && c.block != "" && !c.encoding.IsZero() && c.document.Scope().Valid()
}

type ParseFunc func(ParseContext) (Document, error)

// MarshalFunc writes one carrier block and says what it could not carry as the
// document stated it. Reporting rather than failing is what makes best effort
// the default; a job that would rather fail says so in its policy.
type MarshalFunc func(MarshalContext) (Blob, []loss.Loss, error)

// Encoding is the pure Parse/Marshal behavior attached to one control-plane
// component. It has no Open or payload-grant lifecycle.
type Encoding struct {
	parse     ParseFunc
	marshal   MarshalFunc
	supported []key.ID
	problem   string
}

// WithEncoding attaches metadata behavior without requiring a port shape.
func WithEncoding(parse ParseFunc, marshal MarshalFunc, supported ...key.Erased) plugin.ComponentOption {
	value := newEncoding(parse, marshal, supported)
	return plugin.WithTrait(encodingKey, value.manifest(), plugin.PortShapeOptional, value)
}

// EncodingOf returns the typed metadata behavior attached to component.
func EncodingOf(component plugin.Component) (Encoding, bool) {
	value, ok := plugin.TraitValueOf[Encoding](component, encodingKey)
	return value.clone(), ok
}

func (e Encoding) Valid() bool {
	return e.parse != nil && e.marshal != nil && len(e.supported) != 0 && e.problem == ""
}

// Problem returns the declaration problem, if any.
func (e Encoding) Problem() error {
	if e.problem == "" {
		return nil
	}
	return errors.New(e.problem)
}

// Supports reports whether this encoding directly represents identity. Mapped
// keys are deliberately excluded: Resolver.Project owns that conversion.
func (e Encoding) Supports(identity key.ID) bool {
	index := sort.Search(len(e.supported), func(index int) bool { return e.supported[index].String() >= identity.String() })
	return index < len(e.supported) && e.supported[index] == identity
}

func newEncoding(parse ParseFunc, marshal MarshalFunc, supported []key.Erased) Encoding {
	result := Encoding{parse: parse, marshal: marshal}
	switch {
	case parse == nil:
		result.problem = "metadata encoding requires a Parse function"
		return result
	case marshal == nil:
		result.problem = "metadata encoding requires a Marshal function"
		return result
	case len(supported) == 0:
		result.problem = "metadata encoding must declare at least one directly supported key"
		return result
	}
	seen := make(map[key.ID]struct{}, len(supported))
	for _, declaration := range supported {
		if problem := declaration.Problem(); problem != nil {
			result.problem = problem.Error()
			return result
		}
		if !declaration.Valid() {
			result.problem = "metadata encoding has an invalid directly supported key"
			return result
		}
		identity := declaration.ID()
		if _, exists := seen[identity]; exists {
			result.problem = fmt.Sprintf("metadata encoding directly supports %s more than once", identity)
			return result
		}
		seen[identity] = struct{}{}
		result.supported = append(result.supported, identity)
	}
	sort.Slice(result.supported, func(left, right int) bool { return result.supported[left].String() < result.supported[right].String() })
	return result
}

func (e Encoding) manifest() string {
	if e.problem != "" {
		return "invalid=" + e.problem
	}
	keys := make([]string, len(e.supported))
	for index, identity := range e.supported {
		keys[index] = identity.String()
	}
	return "parse=true|marshal=true|keys=" + strings.Join(keys, ",")
}

func (e Encoding) clone() Encoding {
	e.supported = append([]key.ID(nil), e.supported...)
	return e
}

func (e Encoding) Parse(ctx ParseContext) (Document, error) {
	if !e.Valid() {
		return Document{}, ErrInvalidEncoding
	}
	if !ctx.valid() {
		return Document{}, ErrInvalidContext
	}
	value, err := e.parse(ctx)
	if err != nil {
		return Document{}, err
	}
	if !value.Scope().Valid() || value.Scope() != ctx.scope {
		return Document{}, errors.Join(ErrInvalidEncoding, errors.New("Parse returned a document with the wrong scope"))
	}
	return value, nil
}

func (e Encoding) Marshal(ctx MarshalContext) (Blob, []loss.Loss, error) {
	if !e.Valid() {
		return Blob{}, nil, ErrInvalidEncoding
	}
	if !ctx.valid() {
		return Blob{}, nil, ErrInvalidContext
	}
	value, lost, err := e.marshal(ctx)
	if err != nil {
		return Blob{}, nil, err
	}
	if !value.Valid() {
		return Blob{}, nil, errors.Join(ErrInvalidEncoding, errors.New("Marshal returned an invalid payload"))
	}
	for _, loss := range lost {
		if !loss.Valid() {
			return Blob{}, nil, errors.Join(ErrInvalidEncoding, errors.New("Marshal reported an invalid loss"))
		}
	}
	return value, append([]loss.Loss(nil), lost...), nil
}
