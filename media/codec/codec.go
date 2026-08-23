// Package codec defines codec/parser declarations and composition bindings.
package codec

import (
	"fmt"

	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/plugin"
)

type tagPropertyID struct{}

var tagProperty = property.Define[tagPropertyID](func(value format.Tag) ([]byte, error) {
	if !value.Valid() {
		return nil, fmt.Errorf("codec tag is empty")
	}
	return property.Scalar[format.Tag]()(value)
})

// Tag is the canonical descriptor property used to constrain codec/parser
// candidates selected for a container stream.
func Tag() property.Key[format.Tag] { return tagProperty }

func WithTag(properties property.Set, value format.Tag) (property.Set, error) {
	return tagProperty.Set(properties, value)
}

func TagOf(properties property.Set) (format.Tag, bool) {
	value, ok := tagProperty.Get(properties)
	return value, ok && value.Valid()
}

func Declarations() []plugin.Declaration {
	return []plugin.Declaration{plugin.DeclareKey(tagProperty), plugin.DeclareKey(parameterProperty)}
}

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

// Bind carries the codec tag key declaration, so a plugin that contributes a
// binding never has to remember the vocabulary the binding depends on.
func Bind(key format.Tag, value Codec, parser Parser) Binding {
	targets := []plugin.Identity{value.Identity()}
	if parser.Valid() {
		targets = append(targets, parser.Identity())
	}
	return plugin.Declare[bindingNamespace](key.String(), targets...).WithVocabulary(Declarations()...)
}

func BindWithoutParser(key format.Tag, value Codec) Binding {
	return Bind(key, value, Parser{})
}

func BindingKey(key format.Tag) plugin.DeclarationKey {
	return BindWithoutParser(key, Codec{}).Key()
}

func IsBindingKey(key plugin.DeclarationKey) bool {
	return key.Namespace() == plugin.IdentityOf[bindingNamespace]()
}

func BindingTag(key plugin.DeclarationKey) (format.Tag, bool) {
	if !IsBindingKey(key) {
		return "", false
	}
	value := format.Tag(key.Name())
	return value, value.Valid()
}
