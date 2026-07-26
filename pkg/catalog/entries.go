package catalog

import (
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/registry"
)

type PluginEntry struct {
	Role        string  `json:"role"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
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
	return PluginEntry{Role: string(role), Name: value.RegistryName(), Description: description(value), Fields: fields}, nil
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
