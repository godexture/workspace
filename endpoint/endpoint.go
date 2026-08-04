// Package endpoint defines the small trait layered on normal typed plugin
// components for sessions and devices.
package endpoint

import (
	"errors"

	"github.com/godexture/godec/plugin"
)

// Topology describes whether an endpoint has a finite or live stream shape.
type Topology uint8

const (
	FiniteStatic Topology = iota + 1
	LiveStatic
	LiveDynamic
)

func (t Topology) Valid() bool { return t >= FiniteStatic && t <= LiveDynamic }

// Mode separates realtime presentation from offline processing.
type Mode uint8

const (
	Realtime Mode = iota + 1
	Offline
)

func (m Mode) Valid() bool { return m >= Realtime && m <= Offline }

var (
	ErrInvalidTrait     = errors.New("endpoint trait is invalid")
	ErrInvalidComponent = errors.New("endpoint component is invalid")
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

// Component layers an endpoint trait over an ordinary plugin.Component. The
// plugin component retains port shape and Open; this type adds no registry or
// lifecycle side effects.
type Component struct {
	component plugin.Component
	trait     Trait
}

func New(component plugin.Component, trait Trait) (Component, error) {
	if component.Identity().IsZero() {
		return Component{}, ErrInvalidComponent
	}
	if !trait.Valid() {
		return Component{}, ErrInvalidTrait
	}
	return Component{component: component, trait: trait}, nil
}

func (c Component) Valid() bool                       { return !c.component.Identity().IsZero() && c.trait.Valid() }
func (c Component) Identity() plugin.Identity         { return c.component.Identity() }
func (c Component) PluginComponent() plugin.Component { return c.component }
func (c Component) Trait() Trait                      { return c.trait }
