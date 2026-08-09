package format

import (
	"errors"
	"strings"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/plugin"
)

type readTraitKey struct{}
type writeTraitKey struct{}

var (
	readKey  = plugin.TraitKeyOf[readTraitKey]()
	writeKey = plugin.TraitKeyOf[writeTraitKey]()
)

type ReadTrait struct {
	format       Format
	requirements access.Requirements
	inspect      InspectFunc
}

type InspectFunc func(InspectContext) (Inspection, error)

// Read attaches input byte requirements for one Format component.
func Read(value Format, alternatives ...access.Alternative) plugin.ComponentOption {
	trait := ReadTrait{format: value, requirements: access.NewRequirements(alternatives...)}
	return plugin.WithTrait(readKey, trait.manifest("read"), plugin.PortShapeRequired, trait)
}

// ReadWithInspect attaches input byte requirements and the one pre-Compile
// inspection operation for a Format component.
func ReadWithInspect(value Format, inspect InspectFunc, alternatives ...access.Alternative) plugin.ComponentOption {
	trait := ReadTrait{format: value, requirements: access.NewRequirements(alternatives...), inspect: inspect}
	return plugin.WithTrait(readKey, trait.manifest("read"), plugin.PortShapeRequired, trait)
}

func ReadOf(component plugin.Component) (ReadTrait, bool) {
	return plugin.TraitValueOf[ReadTrait](component, readKey)
}

func (t ReadTrait) Valid() bool {
	return t.format.Valid() && t.requirements.ValidFor(access.SourceDirection)
}
func (t ReadTrait) Format() Format {
	return t.format
}
func (t ReadTrait) Requirements() access.Requirements { return t.requirements.Clone() }
func (t ReadTrait) HasInspect() bool                  { return t.inspect != nil }

func (t ReadTrait) Inspect(ctx InspectContext) (Inspection, error) {
	if !t.Valid() || t.inspect == nil {
		return Inspection{}, ErrInspectUnavailable
	}
	if !ctx.Valid() {
		return Inspection{}, ErrInvalidInspection
	}
	value, err := t.inspect(ctx)
	if err != nil {
		return Inspection{}, err
	}
	if !value.Valid() || value.Format().Identity() != t.format.Identity() {
		return Inspection{}, errors.Join(ErrInvalidInspection, errors.New("inspection Format does not match its read trait"))
	}
	return value, nil
}

func (t ReadTrait) manifest(direction string) string {
	return traitManifest(direction, t.format, t.requirements)
}

type WriteTrait struct {
	format       Format
	requirements access.Requirements
}

// Write attaches output byte requirements for one Format component.
func Write(value Format, alternatives ...access.Alternative) plugin.ComponentOption {
	trait := WriteTrait{format: value, requirements: access.NewRequirements(alternatives...)}
	return plugin.WithTrait(writeKey, trait.manifest("write"), plugin.PortShapeRequired, trait)
}

func WriteOf(component plugin.Component) (WriteTrait, bool) {
	return plugin.TraitValueOf[WriteTrait](component, writeKey)
}

func (t WriteTrait) Valid() bool {
	return t.format.Valid() && t.requirements.ValidFor(access.SinkDirection)
}
func (t WriteTrait) Format() Format {
	return t.format
}
func (t WriteTrait) Requirements() access.Requirements { return t.requirements.Clone() }

func (t WriteTrait) manifest(direction string) string {
	return traitManifest(direction, t.format, t.requirements)
}

func traitManifest(direction string, value Format, requirements access.Requirements) string {
	alternatives := make([]string, len(requirements.Alternatives))
	for index, alternative := range requirements.Alternatives {
		capabilities := make([]string, len(alternative.Capabilities))
		for capabilityIndex, capability := range alternative.Capabilities {
			capabilities[capabilityIndex] = string(capability)
		}
		alternatives[index] = strings.Join(capabilities, "+")
	}
	return direction + "|format=" + value.Identity().String() + "|alternatives=" + strings.Join(alternatives, ",")
}
