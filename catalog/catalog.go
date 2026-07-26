package catalog

import (
	"cmp"
	"fmt"
	"slices"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/registry"
	"github.com/godexture/sdk/cliflag"
)

type Field struct {
	Name      string           `json:"name"`
	Type      string           `json:"type"`
	Help      string           `json:"help"`
	Default   string           `json:"default"`
	Choices   []string         `json:"choices,omitempty"`
	DependsOn *FieldDependency `json:"dependsOn,omitempty"`
}

type FieldDependency struct {
	Field  string   `json:"field"`
	Values []string `json:"values"`
}

type PluginEntry struct {
	Role        string  `json:"role"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
}

// FilterEntry describes a filter's editable configuration and port topology.
// Parameters are resolved before Fields for parameterized filters because they
// may decide which ports and configuration type the concrete filter has.
type FilterEntry struct {
	PluginEntry
	Parameters []Field  `json:"parameters"`
	Inputs     []string `json:"inputs"`
	Outputs    []string `json:"outputs"`
}

type OutputFormat struct {
	Muxer        string   `json:"muxer"`
	Extensions   []string `json:"extensions"`
	Codecs       []string `json:"codecs"`
	DefaultCodec string   `json:"defaultCodec"`
}

type Catalog struct {
	Demuxers []PluginEntry  `json:"demuxers"`
	Decoders []PluginEntry  `json:"decoders"`
	Filters  []FilterEntry  `json:"filters"`
	Encoders []PluginEntry  `json:"encoders"`
	Muxers   []PluginEntry  `json:"muxers"`
	Outputs  []OutputFormat `json:"outputs"`
}

func Build() Catalog {
	return BuildFrom(godec.DefaultRegistry)
}

func BuildFrom(registries registry.Bundle) Catalog {
	result := Catalog{
		Demuxers: entries(manifest.RoleDemuxer, registries.Demuxers),
		Decoders: entries(manifest.RoleDecoder, registries.Decoders),
		Filters:  filterEntries(registries),
		Encoders: entries(manifest.RoleEncoder, registries.Encoders),
		Muxers:   entries(manifest.RoleMuxer, registries.Muxers),
	}
	result.Outputs = outputFormats(registries)
	return result
}

func entries[V registry.Manifest](role manifest.NodeType, values *registry.Registry[V]) []PluginEntry {
	if values == nil {
		return []PluginEntry{}
	}
	result := make([]PluginEntry, 0, len(values.Names()))
	for value := range values.Enumerate() {
		entry, err := pluginEntry(role, value)
		if err == nil {
			result = append(result, entry)
		}
	}
	return result
}

func pluginEntry(role manifest.NodeType, value registry.Manifest) (PluginEntry, error) {
	fields, err := fields(value)
	if err != nil {
		return PluginEntry{}, err
	}
	return PluginEntry{
		Role: string(role), Name: value.RegistryName(),
		Description: description(value), Fields: fields,
	}, nil
}

func fields(value registry.Manifest) ([]Field, error) {
	config, err := value.NewConfiguration()
	if err != nil {
		return nil, err
	}
	described, err := cliflag.DescribeStruct(config)
	if err != nil {
		return nil, err
	}
	result := make([]Field, len(described))
	for i, field := range described {
		var dependsOn *FieldDependency
		if field.DependsOn != nil {
			dependsOn = &FieldDependency{Field: field.DependsOn.Field, Values: slices.Clone(field.DependsOn.Values)}
		}
		result[i] = Field{
			Name: field.Name, Type: field.Type, Help: field.Help,
			Default: field.Default, Choices: slices.Clone(field.Choices), DependsOn: dependsOn,
		}
	}
	return result, nil
}

func filterEntries(registries registry.Bundle) []FilterEntry {
	result := make([]FilterEntry, 0)
	if registries.Filters != nil {
		for value := range registries.Filters.Enumerate() {
			entry, err := filterEntry(value, []Field{})
			if err == nil {
				result = append(result, entry)
			}
		}
	}
	if registries.ParameterizedFilters != nil {
		for value := range registries.ParameterizedFilters.Enumerate() {
			parameters, err := fields(value)
			if err != nil {
				continue
			}
			config, err := value.NewConfiguration()
			if err != nil {
				continue
			}
			manifest, err := value.NewManifest(config)
			if err != nil {
				continue
			}
			entry, err := filterEntry(manifest, parameters)
			if err == nil {
				result = append(result, entry)
			}
		}
	}
	slices.SortFunc(result, func(a, b FilterEntry) int { return cmp.Compare(a.Name, b.Name) })
	return result
}

func filterEntry(value registry.FilterManifest, parameters []Field) (FilterEntry, error) {
	base, err := pluginEntry(manifest.RoleFilter, value)
	if err != nil {
		return FilterEntry{}, err
	}
	inputs := make([]string, 0, len(value.InputRequirements))
	for port := range value.InputRequirements {
		inputs = append(inputs, port)
	}
	slices.Sort(inputs)
	return FilterEntry{
		PluginEntry: base,
		Parameters:  parameters,
		Inputs:      inputs,
		Outputs:     slices.Clone(value.OutputPorts),
	}, nil
}

// DescribeFilter resolves a concrete filter topology for the given structural
// parameters. Ordinary filters reject parameters.
func DescribeFilter(name string, parameters map[string]string) (FilterEntry, error) {
	return DescribeFilterFrom(godec.DefaultRegistry, name, parameters)
}

func DescribeFilterFrom(registries registry.Bundle, name string, parameters map[string]string) (FilterEntry, error) {
	if registries.Filters != nil {
		if value, err := registries.Filters.Lookup(name); err == nil {
			if len(parameters) != 0 {
				return FilterEntry{}, fmt.Errorf("filter %q does not accept parameters", name)
			}
			return filterEntry(value, []Field{})
		}
	}
	if registries.ParameterizedFilters == nil {
		return FilterEntry{}, fmt.Errorf("unknown filter %q", name)
	}
	value, err := registries.ParameterizedFilters.Lookup(name)
	if err != nil {
		return FilterEntry{}, fmt.Errorf("unknown filter %q", name)
	}
	parameterFields, err := fields(value)
	if err != nil {
		return FilterEntry{}, err
	}
	config, err := value.NewConfiguration()
	if err != nil {
		return FilterEntry{}, err
	}
	if err := cliflag.DecodeStruct(config, parameters); err != nil {
		return FilterEntry{}, err
	}
	resolved, err := value.NewManifest(config)
	if err != nil {
		return FilterEntry{}, err
	}
	return filterEntry(resolved, parameterFields)
}

func description(value registry.Manifest) string {
	switch value := value.(type) {
	case registry.MuxerManifest:
		return value.Description
	case registry.DemuxerManifest:
		return value.Description
	case registry.EncoderManifest:
		return value.Description
	case registry.DecoderManifest:
		return value.Description
	case registry.FilterManifest:
		return value.Description
	default:
		return ""
	}
}

func outputFormats(registries registry.Bundle) []OutputFormat {
	if registries.Muxers == nil || registries.Encoders == nil {
		return []OutputFormat{}
	}
	result := make([]OutputFormat, 0, len(registries.Muxers.Names()))
	for muxer := range registries.Muxers.Enumerate() {
		codecs := make([]string, 0, len(muxer.Codecs))
		for _, codec := range muxer.Codecs {
			available := false
			for encoder := range registries.Encoders.Enumerate() {
				if encoder.Supports(codec) {
					available = true
					break
				}
			}
			if available {
				codecs = append(codecs, string(codec))
			}
		}
		if len(codecs) == 0 {
			continue
		}
		defaultCodec := string(muxer.DefaultCodec)
		if !slices.Contains(codecs, defaultCodec) {
			defaultCodec = codecs[0]
		}
		result = append(result, OutputFormat{
			Muxer: muxer.Name, Extensions: slices.Clone(muxer.Extensions),
			Codecs: codecs, DefaultCodec: defaultCodec,
		})
	}
	return result
}
