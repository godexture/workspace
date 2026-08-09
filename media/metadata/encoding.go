package metadata

import (
	"context"
	"errors"

	"github.com/godexture/godec/media/carrier"
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
type MarshalFunc func(MarshalContext) (Blob, error)

// Encoding is the pure Parse/Marshal behavior attached to one control-plane
// component. It has no Open or payload-grant lifecycle.
type Encoding struct {
	parse   ParseFunc
	marshal MarshalFunc
}

// WithEncoding attaches metadata behavior without requiring a port shape.
func WithEncoding(parse ParseFunc, marshal MarshalFunc) plugin.ComponentOption {
	value := Encoding{parse: parse, marshal: marshal}
	return plugin.WithTrait(encodingKey, "parse=true|marshal=true", plugin.PortShapeOptional, value)
}

// EncodingOf returns the typed metadata behavior attached to component.
func EncodingOf(component plugin.Component) (Encoding, bool) {
	value, ok := plugin.TraitValueOf[Encoding](component, encodingKey)
	return value, ok
}

func (e Encoding) Valid() bool { return e.parse != nil && e.marshal != nil }

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

func (e Encoding) Marshal(ctx MarshalContext) (Blob, error) {
	if !e.Valid() {
		return Blob{}, ErrInvalidEncoding
	}
	if !ctx.valid() {
		return Blob{}, ErrInvalidContext
	}
	value, err := e.marshal(ctx)
	if err != nil {
		return Blob{}, err
	}
	if !value.Valid() {
		return Blob{}, errors.Join(ErrInvalidEncoding, errors.New("Marshal returned an invalid payload"))
	}
	return value, nil
}
