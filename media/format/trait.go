package format

import (
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
}

// Read attaches input byte requirements for one Format component.
func Read(value Format, alternatives ...access.Alternative) plugin.ComponentOption {
	trait := ReadTrait{format: value, requirements: access.NewRequirements(alternatives...)}
	return plugin.WithTrait(readKey, trait.manifest("read"), trait)
}

func ReadOf(component plugin.Component) (ReadTrait, bool) {
	return plugin.TraitValueOf[ReadTrait](component, readKey)
}

func (t ReadTrait) Valid() bool { return t.format.Valid() && t.requirements.Valid() }
func (t ReadTrait) Format() Format {
	return t.format
}
func (t ReadTrait) Requirements() access.Requirements { return t.requirements.Clone() }

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
	return plugin.WithTrait(writeKey, trait.manifest("write"), trait)
}

func WriteOf(component plugin.Component) (WriteTrait, bool) {
	return plugin.TraitValueOf[WriteTrait](component, writeKey)
}

func (t WriteTrait) Valid() bool { return t.format.Valid() && t.requirements.Valid() }
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
