package format

import (
	"errors"
	"sort"
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
	probe        ProbeFunc
	inspect      InspectFunc
	fallback     []string
	invalid      bool
}

type ProbeFunc func(ProbeContext) (ProbeResult, error)
type InspectFunc func(InspectContext) (Inspection, error)

type ReadOption func(*readOptions)

type readOptions struct {
	probe      ProbeFunc
	probeSet   bool
	inspect    InspectFunc
	inspectSet bool
	fallback   []string
	invalid    bool
}

// WithProbe adds the bounded operation used before automatic Format
// selection. A Read trait may declare it only once.
func WithProbe(probe ProbeFunc) ReadOption {
	return func(options *readOptions) {
		if options.probeSet {
			options.probe = nil
			return
		}
		options.probeSet = true
		options.probe = probe
	}
}

// WithInspect adds the one selected-Format inspection performed before
// Compile. A Read trait may declare it only once.
func WithInspect(inspect InspectFunc) ReadOption {
	return func(options *readOptions) {
		if options.inspectSet {
			options.inspect = nil
			return
		}
		options.inspectSet = true
		options.inspect = inspect
	}
}

// RequireFallbackConfig declares component config fields that must be present
// in an explicit input hint before an evidence-free Probe fallback may select
// this Format.
func RequireFallbackConfig(fields ...string) ReadOption {
	return func(options *readOptions) {
		seen := make(map[string]struct{}, len(options.fallback)+len(fields))
		for _, field := range options.fallback {
			seen[field] = struct{}{}
		}
		for _, field := range fields {
			field = strings.TrimSpace(field)
			if field == "" {
				options.invalid = true
				continue
			}
			if _, exists := seen[field]; exists {
				options.invalid = true
				continue
			}
			seen[field] = struct{}{}
			options.fallback = append(options.fallback, field)
		}
		sort.Strings(options.fallback)
	}
}

// Read attaches input byte requirements and optional pre-Compile operations
// for one Format component.
func Read(value Format, requirements access.Requirements, values ...ReadOption) plugin.ComponentOption {
	options := readOptions{}
	for _, option := range values {
		if option != nil {
			option(&options)
		}
	}
	trait := ReadTrait{
		format:       value,
		requirements: requirements.Clone(),
		probe:        options.probe,
		inspect:      options.inspect,
		fallback:     append([]string(nil), options.fallback...),
		invalid:      options.invalid || options.probeSet && options.probe == nil || options.inspectSet && options.inspect == nil || len(options.fallback) != 0 && options.probe == nil,
	}
	return plugin.WithTrait(readKey, trait.manifest("read"), plugin.PortShapeRequired, trait)
}

func ReadOf(component plugin.Component) (ReadTrait, bool) {
	return plugin.TraitValueOf[ReadTrait](component, readKey)
}

func (t ReadTrait) Valid() bool {
	return !t.invalid && t.format.Valid() && t.requirements.ValidFor(access.SourceDirection)
}
func (t ReadTrait) Format() Format {
	return t.format
}
func (t ReadTrait) Requirements() access.Requirements { return t.requirements.Clone() }
func (t ReadTrait) HasProbe() bool                    { return t.probe != nil }
func (t ReadTrait) HasInspect() bool                  { return t.inspect != nil }
func (t ReadTrait) FallbackConfigFields() []string    { return append([]string(nil), t.fallback...) }

func (t ReadTrait) Probe(ctx ProbeContext) (ProbeResult, error) {
	if !t.Valid() || t.probe == nil {
		return ProbeResult{}, ErrProbeUnavailable
	}
	if !ctx.Valid() {
		return ProbeResult{}, ErrInvalidProbe
	}
	value, err := t.probe(ctx)
	if err != nil {
		return ProbeResult{}, err
	}
	if !value.Valid() {
		return ProbeResult{}, ErrInvalidProbe
	}
	return value, nil
}

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
	return traitManifest(direction, t.format, t.requirements) + "|probe=" + boolManifest(t.probe != nil) + "|inspect=" + boolManifest(t.inspect != nil) + "|fallbackConfig=" + strings.Join(t.fallback, ",")
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
	return direction + "|" + value.manifest() + "|alternatives=" + strings.Join(alternatives, ",")
}

func boolManifest(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
