package catalog

import (
	"fmt"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/registry"
	setting "github.com/godexture/sdk/config"
)

func ResolveConfiguration(role, name string, parameters, values map[string]string) (setting.Resolution, error) {
	return ResolveConfigurationFrom(godec.DefaultRegistry, role, name, parameters, values)
}

func ResolveConfigurationFrom(registries registry.Bundle, role, name string, parameters, values map[string]string) (setting.Resolution, error) {
	var value registry.Manifest
	var err error
	switch manifest.NodeType(role) {
	case manifest.RoleFilter:
		value, _, err = resolveFilterManifest(registries, name, parameters)
	case manifest.RoleDemuxer:
		value, err = lookupManifest(registries.Demuxers, role, name, parameters)
	case manifest.RoleDecoder:
		value, err = lookupManifest(registries.Decoders, role, name, parameters)
	case manifest.RoleEncoder:
		value, err = lookupManifest(registries.Encoders, role, name, parameters)
	case manifest.RoleMuxer:
		value, err = lookupManifest(registries.Muxers, role, name, parameters)
	default:
		return setting.Resolution{}, fmt.Errorf("unknown plugin role %q", role)
	}
	if err != nil {
		return setting.Resolution{}, err
	}
	_, resolution, err := setting.Resolve(value, values, setting.Draft)
	return resolution, err
}

func lookupManifest[V registry.Manifest](values *registry.Registry[V], role, name string, parameters map[string]string) (registry.Manifest, error) {
	if len(parameters) != 0 {
		return nil, fmt.Errorf("%s %q does not accept parameters", role, name)
	}
	if values == nil {
		return nil, fmt.Errorf("unknown %s %q", role, name)
	}
	value, err := values.Lookup(name)
	if err != nil {
		return nil, fmt.Errorf("unknown %s %q", role, name)
	}
	return value, nil
}
