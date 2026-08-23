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
	return []plugin.Declaration{plugin.DeclareKey(tagProperty), plugin.DeclareKey(parameterProperty), plugin.DeclareKey(blockProperty)}
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

// A codec tag names a codec, not a direction. The components that implement it
// are declared by role, so a composition states the one that reads a tag and
// the one that writes it as what they are, and a planner can narrow candidates
// travelling either way. Each role is its own declaration namespace, which is
// also what lets one tag name several components: four decoders for one wire
// coding are one declaration, not four conflicting ones.
type (
	decoderNamespace struct{}
	encoderNamespace struct{}
	parserNamespace  struct{}
)

// BindDecoder declares the components that read a container codec tag.
func BindDecoder(key format.Tag, values ...Codec) Binding {
	return bind[decoderNamespace](key, identities(values))
}

// BindEncoder declares the components that write a container codec tag.
func BindEncoder(key format.Tag, values ...Codec) Binding {
	return bind[encoderNamespace](key, identities(values))
}

// BindParser declares the component that cuts a container codec tag into
// packets. Framing is one component per tag: a stream is cut one way.
func BindParser(key format.Tag, value Parser) Binding {
	return bind[parserNamespace](key, []plugin.Identity{value.Identity()})
}

func bind[Namespace any](key format.Tag, targets []plugin.Identity) Binding {
	return plugin.Declare[Namespace](key.String(), targets...).WithVocabulary(Declarations()...)
}

func identities(values []Codec) []plugin.Identity {
	result := make([]plugin.Identity, 0, len(values))
	for _, value := range values {
		result = append(result, value.Identity())
	}
	return result
}

// DecoderKey, EncoderKey and ParserKey name the declaration one role of one tag
// produces, which is how a catalog recognizes a binding it did not construct.
func DecoderKey(key format.Tag) plugin.DeclarationKey { return BindDecoder(key).Key() }
func EncoderKey(key format.Tag) plugin.DeclarationKey { return BindEncoder(key).Key() }
func ParserKey(key format.Tag) plugin.DeclarationKey  { return BindParser(key, Parser{}).Key() }

// BindingTag returns the tag a codec declaration names, and the role it names
// it in. It reports false for a declaration in any other namespace.
func BindingTag(key plugin.DeclarationKey) (format.Tag, Role, bool) {
	role := roleOf(key.Namespace())
	if role == 0 {
		return "", 0, false
	}
	value := format.Tag(key.Name())
	return value, role, value.Valid()
}

// Role is which side of a codec a component implements.
type Role uint8

const (
	DecoderRole Role = iota + 1
	EncoderRole
	ParserRole
)

func roleOf(namespace plugin.Identity) Role {
	switch namespace {
	case plugin.IdentityOf[decoderNamespace]():
		return DecoderRole
	case plugin.IdentityOf[encoderNamespace]():
		return EncoderRole
	case plugin.IdentityOf[parserNamespace]():
		return ParserRole
	default:
		return 0
	}
}

func (r Role) String() string {
	switch r {
	case DecoderRole:
		return "decoder"
	case EncoderRole:
		return "encoder"
	case ParserRole:
		return "parser"
	default:
		return "unknown codec role"
	}
}

// WithoutTag drops the codec tag, which is what a decoded stream does: it is
// no longer the coded stream that tag named.
func WithoutTag(properties property.Set) property.Set {
	return properties.Delete(tagProperty.ID())
}

// Described is a stream description that carries properties, which is what a
// suggestion needs of it to read the tag a consumer asked for.
type Described interface{ Properties() property.Set }

// DemandedTag reads the codec tag a consumer named in the stream it asked for.
// A coder does not know what its container calls it, so it takes the name from
// the request. What keeps a coder from answering to a name that is not its own
// is the composition: a binding states which component implements a tag, and a
// planner narrows candidates to it.
func DemandedTag[D Described](suggestion plugin.Suggestion[D]) format.Tag {
	for _, demand := range suggestion.Demands() {
		desired, ok := demand.Need().Desired()
		if !ok {
			continue
		}
		if tag, tagged := TagOf(desired.Properties()); tagged {
			return tag
		}
	}
	return ""
}
