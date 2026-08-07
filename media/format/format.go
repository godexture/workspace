// Package format defines open container/elementary-stream contracts.
package format

import (
	"errors"
	"fmt"

	"github.com/godexture/godec/access"
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

type Capability = access.Capability

const (
	SequentialRead = access.SequentialRead
	RandomRead     = access.RandomRead
	StableSize     = access.StableSize
	Reopen         = access.Reopen
	ConcurrentRead = access.ConcurrentRead
	CancelableRead = access.CancelableRead
)

type Alternative = access.Alternative

func AnyOf(capabilities ...Capability) Alternative {
	return access.AnyOf(capabilities...)
}

// Format is a declarative format contract. It does not own byte locations,
// decoder implementations, metadata parsers, or access providers.
type Format struct {
	identity     plugin.Identity
	alternatives []access.Alternative
	carriers     []carrier.ID
	packetized   bool
}

func Define[Marker any](alternatives []access.Alternative, carriers []carrier.ID) (Format, error) {
	result := Format{identity: plugin.IdentityOf[Marker]()}
	if err := result.set(alternatives, carriers); err != nil {
		return Format{}, err
	}
	return result, nil
}

func DefinePacketized[Marker any](carriers []carrier.ID) (Format, error) {
	result := Format{identity: plugin.IdentityOf[Marker](), packetized: true}
	if err := result.set(nil, carriers); err != nil {
		return Format{}, err
	}
	return result, nil
}

func (f *Format) set(alternatives []access.Alternative, carriers []carrier.ID) error {
	if f.identity.IsZero() {
		return errors.New("format marker identity must be valid")
	}
	if len(alternatives) == 0 && !f.packetized {
		return errors.New("format must declare a capability alternative")
	}
	for index, alternative := range alternatives {
		if len(alternative.Capabilities) == 0 {
			return fmt.Errorf("format capability alternative %d is empty", index)
		}
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
	f.alternatives = cloneAlternatives(alternatives)
	f.carriers = append([]carrier.ID(nil), carriers...)
	return nil
}

func (f Format) Valid() bool                        { return !f.identity.IsZero() }
func (f Format) Identity() plugin.Identity          { return f.identity }
func (f Format) Alternatives() []access.Alternative { return cloneAlternatives(f.alternatives) }
func (f Format) Carriers() []carrier.ID             { return append([]carrier.ID(nil), f.carriers...) }
func (f Format) Packetized() bool                   { return f.packetized }

func cloneAlternatives(values []access.Alternative) []access.Alternative {
	result := make([]access.Alternative, len(values))
	for index, value := range values {
		result[index] = value.Clone()
	}
	return result
}
