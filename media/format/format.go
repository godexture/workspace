// Package format defines open container/elementary-stream contracts.
package format

import (
	"errors"
	"fmt"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/plugin"
)

type Tag string

func NewTag(namespace, value string) Tag {
	if namespace == "" {
		return Tag(value)
	}
	if value == "" {
		return Tag(namespace)
	}
	return Tag(namespace + ":" + value)
}

func (t Tag) Valid() bool    { return t != "" }
func (t Tag) String() string { return string(t) }

// Format is a declarative format contract. It does not own byte locations,
// decoder implementations, metadata parsers, or access providers.
type Format struct {
	identity   plugin.Identity
	carriers   []carrier.ID
	packetized bool
}

func Define[Marker any](carriers []carrier.ID) (Format, error) {
	result := Format{identity: plugin.IdentityOf[Marker]()}
	if err := result.set(carriers); err != nil {
		return Format{}, err
	}
	return result, nil
}

func DefinePacketized[Marker any](carriers []carrier.ID) (Format, error) {
	result := Format{identity: plugin.IdentityOf[Marker](), packetized: true}
	if err := result.set(carriers); err != nil {
		return Format{}, err
	}
	return result, nil
}

func (f *Format) set(carriers []carrier.ID) error {
	if f.identity.IsZero() {
		return errors.New("format marker identity must be valid")
	}
	seen := make(map[carrier.ID]struct{}, len(carriers))
	for _, id := range carriers {
		if !id.Valid() {
			return errors.New("format carrier identity is required")
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("format carrier %q is repeated", id)
		}
		seen[id] = struct{}{}
	}
	f.carriers = append([]carrier.ID(nil), carriers...)
	return nil
}

func (f Format) Valid() bool               { return !f.identity.IsZero() }
func (f Format) Identity() plugin.Identity { return f.identity }
func (f Format) Carriers() []carrier.ID    { return append([]carrier.ID(nil), f.carriers...) }
func (f Format) Packetized() bool          { return f.packetized }
