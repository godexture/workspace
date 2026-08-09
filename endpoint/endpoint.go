// Package endpoint defines the small trait layered on normal typed plugin
// components for sessions and devices.
package endpoint

import (
	"errors"

	"github.com/godexture/godec/plugin"
)

type componentTraitKey struct{}

var traitKey = plugin.TraitKeyOf[componentTraitKey]()

// Topology describes whether an endpoint has a finite or live stream shape.
type Topology uint8

const (
	FiniteStatic Topology = iota + 1
	LiveStatic
	LiveDynamic
)

func (t Topology) Valid() bool { return t >= FiniteStatic && t <= LiveDynamic }

func (t Topology) String() string {
	switch t {
	case FiniteStatic:
		return "finite-static"
	case LiveStatic:
		return "live-static"
	case LiveDynamic:
		return "live-dynamic"
	default:
		return "unknown"
	}
}

// Mode separates realtime presentation from offline processing.
type Mode uint8

const (
	Realtime Mode = iota + 1
	Offline
)

func (m Mode) Valid() bool { return m >= Realtime && m <= Offline }

func (m Mode) String() string {
	switch m {
	case Realtime:
		return "realtime"
	case Offline:
		return "offline"
	default:
		return "unknown"
	}
}

var (
	ErrInvalidTrait = errors.New("endpoint trait is invalid")
)

// Trait is the M3 endpoint contract. Clock, latency, reconnect, hotplug, and
// external-effect policy are intentionally left for endpoint consumers.
type Trait struct {
	topology Topology
	mode     Mode
}

func NewTrait(topology Topology, mode Mode) (Trait, error) {
	if !topology.Valid() || !mode.Valid() {
		return Trait{}, ErrInvalidTrait
	}
	return Trait{topology: topology, mode: mode}, nil
}

func (t Trait) Valid() bool        { return t.topology.Valid() && t.mode.Valid() }
func (t Trait) Topology() Topology { return t.topology }
func (t Trait) Mode() Mode         { return t.mode }

// WithTrait attaches endpoint behavior to a normal typed component. The
// component's directional shape determines whether it is a source or sink.
func WithTrait(trait Trait) plugin.ComponentOption {
	manifest := "topology=" + trait.topology.String() + "|mode=" + trait.mode.String()
	return plugin.WithTrait(traitKey, manifest, trait)
}

// TraitOf returns the typed endpoint trait attached to component.
func TraitOf(component plugin.Component) (Trait, bool) {
	trait, ok := plugin.TraitValueOf[Trait](component, traitKey)
	return trait, ok
}
