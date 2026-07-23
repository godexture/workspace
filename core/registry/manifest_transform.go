package registry

import (
	"fmt"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
)

type TransformManifest struct {
	BaseManifest
	InputRequirements   InputRequirements
	ProfileRequirements ProfileRequirements
	Resources           ResourceRequest
}

type InputRequirementsFunc func(target media.CodecID, config Configuration) ([]manifest.Capability, error)

type InputRequirements map[string]InputRequirementsFunc

// ProfileRequirements can refine a port's requirements using the profiles of
// the streams already connected to the transform. A profile requirement takes
// precedence over the static requirement for the same port.
type ProfileRequirementsFunc func(inputs media.StreamSet, target media.CodecID, config Configuration) ([]manifest.Capability, error)

type ProfileRequirements map[string]ProfileRequirementsFunc

func StaticRequirements(capabilities ...manifest.Capability) InputRequirementsFunc {
	return func(media.CodecID, Configuration) ([]manifest.Capability, error) {
		return capabilities, nil
	}
}

func SingleInputRequirements(requirements InputRequirementsFunc) InputRequirements {
	return InputRequirements{"in": requirements}
}

func (m TransformManifest) Requirements(port string, target media.CodecID, config Configuration) ([]manifest.Capability, error) {
	return m.requirements(port, nil, target, config)
}

func (m TransformManifest) RequirementsFor(port string, inputs media.StreamSet, target media.CodecID, config Configuration) ([]manifest.Capability, error) {
	return m.requirements(port, inputs, target, config)
}

func (m TransformManifest) requirements(port string, inputs media.StreamSet, target media.CodecID, config Configuration) ([]manifest.Capability, error) {
	if profile := m.ProfileRequirements[port]; profile != nil && inputs != nil {
		requirements, err := profile(inputs, target, config)
		if err != nil {
			return nil, err
		}
		return m.validateRequirements(port, requirements)
	}
	resolver, ok := m.InputRequirements[port]
	if !ok || resolver == nil {
		return nil, fmt.Errorf("transform manifest %q has no input requirements for port %q", m.Name, port)
	}
	requirements, err := resolver(target, config)
	if err != nil {
		return nil, err
	}
	return m.validateRequirements(port, requirements)
}

func (m TransformManifest) validateRequirements(port string, requirements []manifest.Capability) ([]manifest.Capability, error) {
	if len(requirements) == 0 {
		return nil, fmt.Errorf("transform manifest %q has no input requirements", m.Name)
	}
	for i, capability := range requirements {
		if manifest.IsNilCapability(capability) {
			return nil, fmt.Errorf("transform manifest %q input requirement %d is nil", m.Name, i)
		}
	}
	return requirements, nil
}

func (m TransformManifest) Accept(port string, stream media.StreamInfo, target media.CodecID, config Configuration) (bool, error) {
	requirements, err := m.Requirements(port, target, config)
	if err != nil {
		return false, err
	}
	return manifest.MatchesAny(requirements, stream), nil
}

func (m TransformManifest) validate() error {
	if err := m.BaseManifest.validate(); err != nil {
		return err
	}
	if len(m.InputRequirements) == 0 {
		return fmt.Errorf("transform manifest %q must declare input requirements", m.Name)
	}
	for port, requirements := range m.InputRequirements {
		if port == "" || requirements == nil {
			return fmt.Errorf("transform manifest %q has invalid input requirements for port %q", m.Name, port)
		}
	}
	for port, requirements := range m.ProfileRequirements {
		if _, ok := m.InputRequirements[port]; !ok || requirements == nil {
			return fmt.Errorf("transform manifest %q has invalid profile requirements for port %q", m.Name, port)
		}
	}
	return nil
}
