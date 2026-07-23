package catalog

import (
	"slices"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/registry"
	"github.com/godexture/sdk/cliflag"
)

type Field struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Help    string   `json:"help"`
	Default string   `json:"default"`
	Choices []string `json:"choices,omitempty"`
}

type PluginEntry struct {
	Role        string  `json:"role"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
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
	Filters  []PluginEntry  `json:"filters"`
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
		Filters:  entries(manifest.RoleFilter, registries.Filters),
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
		config, err := value.NewConfiguration()
		if err != nil {
			continue
		}
		described, err := cliflag.DescribeStruct(config)
		if err != nil {
			continue
		}
		fields := make([]Field, len(described))
		for i, field := range described {
			fields[i] = Field{
				Name: field.Name, Type: field.Type, Help: field.Help,
				Default: field.Default, Choices: slices.Clone(field.Choices),
			}
		}
		result = append(result, PluginEntry{
			Role: string(role), Name: value.RegistryName(),
			Description: description(value), Fields: fields,
		})
	}
	return result
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
