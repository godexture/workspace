// Package stream defines immutable stream-local descriptors.
package stream

import (
	"errors"

	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/timing"
)

var ErrInvalidDescriptor = errors.New("stream descriptor has an invalid schema or time base")

// Descriptor contains immutable information shared by all items in one
// stream. It intentionally has no media-kind enum.
type Descriptor struct {
	schema     schema.ID
	timeBase   timing.Base
	properties property.Set
	metadata   metadata.Document
}

func NewDescriptor(identity schema.ID, timeBase timing.Base, properties property.Set) (Descriptor, error) {
	if identity.IsZero() || !timeBase.Valid() {
		return Descriptor{}, ErrInvalidDescriptor
	}
	return Descriptor{schema: identity, timeBase: timeBase, properties: properties}, nil
}

func MustDescriptor(identity schema.ID, timeBase timing.Base, properties property.Set) Descriptor {
	descriptor, err := NewDescriptor(identity, timeBase, properties)
	if err != nil {
		panic(err)
	}
	return descriptor
}

func (d Descriptor) Valid() bool              { return !d.schema.IsZero() && d.timeBase.Valid() }
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
