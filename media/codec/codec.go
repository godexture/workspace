// Package codec defines codec/parser declarations and composition bindings.
package codec

import (
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin"
)

// Codec is a component identity for a media codec. The implementation is
// opened through plugin.Component; this type only participates in
// composition declarations.
type Codec struct {
	identity plugin.Identity
}

func New(identity plugin.Identity) Codec { return Codec{identity: identity} }

func Define[Marker any]() Codec { return New(plugin.IdentityOf[Marker]()) }

func (c Codec) Valid() bool               { return !c.identity.IsZero() }
func (c Codec) Identity() plugin.Identity { return c.identity }

// Parser is a first-class component identity for a packetizer. Its
// implementation is returned by the component Open contract.
type Parser struct {
	identity plugin.Identity
}

func NewParser(identity plugin.Identity) Parser { return Parser{identity: identity} }

func DefineParser[Marker any]() Parser { return NewParser(plugin.IdentityOf[Marker]()) }

func (p Parser) Valid() bool               { return !p.identity.IsZero() }
func (p Parser) Identity() plugin.Identity { return p.identity }

// Binding is the codec namespace declaration retained by plugin.Set.
type Binding = plugin.Declaration

type bindingNamespace struct{}

func Bind(key format.Tag, value Codec, parser Parser) Binding {
	targets := []plugin.Identity{value.Identity()}
	if parser.Valid() {
		targets = append(targets, parser.Identity())
	}
	return plugin.Declare[bindingNamespace](key.String(), targets...)
}

func BindWithoutParser(key format.Tag, value Codec) Binding {
	return Bind(key, value, Parser{})
}
