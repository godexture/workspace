package endpoint

import (
	"errors"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/plugin"
)

var ErrInvalidDevice = errors.New("endpoint device is invalid")

// DeviceDescriptor is an observation of one physical or OS-backed device.
// It is separate from the endpoint component and device reference.
type DeviceDescriptor struct {
	Name       string
	Properties property.Set
}

// Device identifies a selected device without scanning or opening it.
type Device struct {
	component  plugin.Identity
	reference  access.Reference
	descriptor DeviceDescriptor
}

func NewDevice(component plugin.Identity, reference access.Reference, descriptor DeviceDescriptor) (Device, error) {
	if component.IsZero() || !reference.Valid() {
		return Device{}, ErrInvalidDevice
	}
	return Device{component: component, reference: reference, descriptor: descriptor}, nil
}

func (d Device) Valid() bool                        { return !d.component.IsZero() && d.reference.Valid() }
func (d Device) ComponentIdentity() plugin.Identity { return d.component }
func (d Device) Reference() access.Reference        { return d.reference }
func (d Device) Descriptor() DeviceDescriptor       { return d.descriptor }
