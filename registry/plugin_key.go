package registry

import (
	"fmt"
	"reflect"

	"github.com/godexture/core/domain/manifest"
)

// PluginKey is the registry-assigned identity of a plugin implementation.
// Its fields are intentionally private: plugins identify themselves only by
// registering a named configuration type for a manifest role.
type PluginKey struct {
	role       manifest.NodeType
	configType reflect.Type
}

func (k PluginKey) Role() manifest.NodeType {
	return k.role
}

func (k PluginKey) ConfigurationType() reflect.Type {
	return k.configType
}

func (k PluginKey) String() string {
	if k.configType == nil {
		return string(k.role) + ":<invalid>"
	}
	return fmt.Sprintf("%s:%s.%s", k.role, k.configType.PkgPath(), k.configType.Name())
}

func pluginKey(role manifest.NodeType, config Configuration) (PluginKey, error) {
	configType, err := configurationType(config)
	if err != nil {
		return PluginKey{}, err
	}
	return PluginKey{role: role, configType: configType}, nil
}

func configurationType(config Configuration) (reflect.Type, error) {
	if config == nil {
		return nil, fmt.Errorf("plugin configuration must not be nil")
	}

	configType := reflect.TypeOf(config)
	configValue := reflect.ValueOf(config)
	if configValue.Kind() == reflect.Pointer && configValue.IsNil() {
		return nil, fmt.Errorf("plugin configuration must not be a typed nil pointer: %s", configType)
	}
	for configType.Kind() == reflect.Pointer {
		configType = configType.Elem()
	}

	if configType.Kind() == reflect.Interface ||
		configType.Name() == "" ||
		configType.PkgPath() == "" {
		return nil, fmt.Errorf(
			"plugin configuration must be a named concrete type declared by a package: %s",
			configType,
		)
	}
	return configType, nil
}
