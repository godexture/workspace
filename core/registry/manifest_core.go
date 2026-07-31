package registry

import (
	"fmt"

	"github.com/godexture/godec/core/domain/manifest"
)

func assignManifestID[V Manifest](manifest V, key PluginKey) (V, error) {
	switch m := any(manifest).(type) {
	case BaseManifest:
		m.key = key
		return any(m).(V), nil

	case TransformManifest:
		m.BaseManifest.key = key
		return any(m).(V), nil

	case MuxerManifest:
		m.BaseManifest.key = key
		return any(m).(V), nil

	case DemuxerManifest:
		m.BaseManifest.key = key
		return any(m).(V), nil

	case EncoderManifest:
		m.TransformManifest.BaseManifest.key = key
		return any(m).(V), nil

	case DecoderManifest:
		m.TransformManifest.BaseManifest.key = key
		return any(m).(V), nil

	case FilterManifest:
		m.TransformManifest.BaseManifest.key = key
		return any(m).(V), nil

	case ParameterizedFilterManifest:
		m.BaseManifest.key = key
		return any(m).(V), nil

	default:
		return manifest, fmt.Errorf("invalid manifest type: %T", manifest)
	}
}

func manifestRole[V Manifest]() manifest.NodeType {
	var value V
	switch any(value).(type) {
	case MuxerManifest:
		return manifest.RoleMuxer
	case DemuxerManifest:
		return manifest.RoleDemuxer
	case EncoderManifest:
		return manifest.RoleEncoder
	case DecoderManifest:
		return manifest.RoleDecoder
	case FilterManifest:
		return manifest.RoleFilter
	case ParameterizedFilterManifest:
		return manifest.RoleFilter
	default:
		panic(fmt.Sprintf("unsupported registry manifest type: %T", value))
	}
}
