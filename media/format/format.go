// Package format defines open container/elementary-stream contracts.
package format

import (
	"errors"
	"fmt"
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

type CarrierID string

func (id CarrierID) Valid() bool    { return id != "" }
func (id CarrierID) String() string { return string(id) }

// CarrierOwner is an open string rather than a closed owner enum. A carrier
// can be declared by a format or by a codec/bitstream family.
type Carrier struct {
	Identity CarrierID
	Owner    string
}

func NewCarrier(identity CarrierID, owner string) Carrier {
	return Carrier{Identity: identity, Owner: owner}
}

type Capability string

const (
	SequentialRead Capability = "sequential-read"
	RandomRead     Capability = "random-read"
	StableSize     Capability = "stable-size"
	Reopen         Capability = "reopen"
	ConcurrentRead Capability = "concurrent-read"
)

type Alternative struct{ Capabilities []Capability }

func AnyOf(capabilities ...Capability) Alternative {
	return Alternative{Capabilities: append([]Capability(nil), capabilities...)}
}

func (a Alternative) Clone() Alternative { return AnyOf(a.Capabilities...) }

// Format is a declarative format contract. It does not own byte locations,
// decoder implementations, metadata parsers, or access providers.
type Format struct {
	identity     Tag
	alternatives []Alternative
	carriers     []Carrier
	packetized   bool
}

func New(identity Tag, alternatives []Alternative, carriers []Carrier) (Format, error) {
	result := Format{identity: identity, packetized: false}
	if err := result.set(alternatives, carriers); err != nil {
		return Format{}, err
	}
	return result, nil
}

func NewPacketized(identity Tag, carriers []Carrier) (Format, error) {
	result := Format{identity: identity, packetized: true}
	if err := result.set(nil, carriers); err != nil {
		return Format{}, err
	}
	return result, nil
}

func (f *Format) set(alternatives []Alternative, carriers []Carrier) error {
	if !f.identity.Valid() {
		return errors.New("format identity must not be empty")
	}
	if len(alternatives) == 0 && !f.packetized {
		return errors.New("format must declare a capability alternative")
	}
	for index, alternative := range alternatives {
		if len(alternative.Capabilities) == 0 {
			return fmt.Errorf("format capability alternative %d is empty", index)
		}
	}
	seen := make(map[CarrierID]struct{}, len(carriers))
	for _, carrier := range carriers {
		if !carrier.Identity.Valid() || carrier.Owner == "" {
			return errors.New("format carrier identity and owner are required")
		}
		if _, ok := seen[carrier.Identity]; ok {
			return fmt.Errorf("format carrier %q is repeated", carrier.Identity)
		}
		seen[carrier.Identity] = struct{}{}
	}
	f.alternatives = cloneAlternatives(alternatives)
	f.carriers = append([]Carrier(nil), carriers...)
	return nil
}

func (f Format) Valid() bool                 { return f.identity.Valid() }
func (f Format) Identity() Tag               { return f.identity }
func (f Format) Alternatives() []Alternative { return cloneAlternatives(f.alternatives) }
func (f Format) Carriers() []Carrier         { return append([]Carrier(nil), f.carriers...) }
func (f Format) Packetized() bool            { return f.packetized }

func cloneAlternatives(values []Alternative) []Alternative {
	result := make([]Alternative, len(values))
	for index, value := range values {
		result[index] = value.Clone()
	}
	return result
}
