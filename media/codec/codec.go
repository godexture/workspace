// Package codec defines parser/codec declarations and composition bindings.
package codec

import (
	"context"
	"errors"

	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
)

type ID string

func (id ID) Valid() bool    { return id != "" }
func (id ID) String() string { return string(id) }

type Codec struct {
	identity ID
}

func New(identity ID) Codec  { return Codec{identity: identity} }
func (c Codec) Valid() bool  { return c.identity.Valid() }
func (c Codec) Identity() ID { return c.identity }

type ParserFunc func(context.Context, packet.Chunk) ([]packet.Packet, error)

// Parser is a first-class packetizer declaration. A zero Parser means that a
// format is already packetized and no parser is required.
type Parser struct {
	identity ID
	parse    ParserFunc
}

func NewParser(identity ID, parse ParserFunc) Parser { return Parser{identity: identity, parse: parse} }
func (p Parser) Valid() bool                         { return p.identity.Valid() }
func (p Parser) Identity() ID                        { return p.identity }
func (p Parser) Parse(ctx context.Context, chunk packet.Chunk) ([]packet.Packet, error) {
	if p.parse == nil {
		return nil, errors.New("parser has no implementation")
	}
	return p.parse(ctx, chunk)
}

type Target struct {
	Codec  ID
	Parser ID
}

type Binding struct {
	key    format.Tag
	codec  Codec
	parser Parser
}

func Bind(key format.Tag, value Codec, parser Parser) Binding {
	return Binding{key: key, codec: value, parser: parser}
}

func BindWithoutParser(key format.Tag, value Codec) Binding {
	return Bind(key, value, Parser{})
}

func (b Binding) Valid() bool {
	return b.key.Valid() && b.codec.Valid() && (!b.parser.Valid() || b.parser.Identity().Valid())
}
func (b Binding) Key() format.Tag { return b.key }
func (b Binding) Codec() Codec    { return b.codec }
func (b Binding) Parser() (Parser, bool) {
	return b.parser, b.parser.Valid()
}
func (b Binding) Target() Target {
	return Target{Codec: b.codec.Identity(), Parser: b.parser.Identity()}
}
