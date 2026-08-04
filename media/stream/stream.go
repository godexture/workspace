// Package stream defines immutable stream-local descriptors.
package stream

import (
	"errors"

	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/timing"
)

var (
	ErrInvalidDescriptor = errors.New("stream descriptor has an invalid schema or time base")
	ErrInvalidID         = errors.New("stream id must not be empty")
)

// ID names one stream within the topology a source exposes.
//
// It is deliberately not a schema.ID: a schema names the kind of unit a stream
// carries, so two audio streams share one schema identity and could not be told
// apart by it. Whoever inspects the topology assigns these; core never
// interprets the value.
type ID string

func (i ID) IsZero() bool   { return i == "" }
func (i ID) String() string { return string(i) }

// Descriptor contains immutable information shared by all items in one
// stream. It intentionally has no media-kind enum.
type Descriptor struct {
	id         ID
	schema     schema.ID
	timeBase   timing.Base
	properties property.Set
	metadata   metadata.Document
}

func NewDescriptor(id ID, identity schema.ID, timeBase timing.Base, properties property.Set) (Descriptor, error) {
	if id.IsZero() {
		return Descriptor{}, ErrInvalidID
	}
	if identity.IsZero() || !timeBase.Valid() {
		return Descriptor{}, ErrInvalidDescriptor
	}
	return Descriptor{id: id, schema: identity, timeBase: timeBase, properties: properties}, nil
}

func MustDescriptor(id ID, identity schema.ID, timeBase timing.Base, properties property.Set) Descriptor {
	descriptor, err := NewDescriptor(id, identity, timeBase, properties)
	if err != nil {
		panic(err)
	}
	return descriptor
}

func (d Descriptor) Valid() bool              { return !d.id.IsZero() && !d.schema.IsZero() && d.timeBase.Valid() }
func (d Descriptor) ID() ID                   { return d.id }
func (d Descriptor) Schema() schema.ID        { return d.schema }
func (d Descriptor) TimeBase() timing.Base    { return d.timeBase }
func (d Descriptor) Properties() property.Set { return d.properties }

// Metadata returns the static document describing this stream. It is empty
// when the source carried none. Metadata that varies over time belongs in a
// typed event stream, not here.
func (d Descriptor) Metadata() metadata.Document { return d.metadata }

// WithMetadata returns a copy carrying document. The receiver is unchanged, so
// a descriptor handed to a component cannot gain metadata behind its back.
func (d Descriptor) WithMetadata(document metadata.Document) Descriptor {
	d.metadata = document
	return d
}
