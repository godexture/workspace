// Package stream defines immutable stream-local descriptors.
package stream

import (
	"errors"

	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/timing"
)

var ErrInvalidDescriptor = errors.New("stream descriptor has an invalid schema or time base")

// Descriptor contains immutable information shared by all items in one
// stream. It intentionally has no media-kind enum or metadata field yet.
type Descriptor struct {
	schema     schema.ID
	timeBase   timing.Base
	properties property.Set
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
