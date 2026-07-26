package registry

import (
	"fmt"
	"reflect"
)

type BaseManifest struct {
	key                  PluginKey
	Name                 string
	Description          string
	ConfigurationFactory ConfigurationFactory
}

func (m BaseManifest) ID() PluginKey { return m.key }

func (m BaseManifest) RegistryName() string { return m.Name }

func (m BaseManifest) ConfigurationType() reflect.Type {
	if m.ConfigurationFactory == nil {
		return nil
	}
	return m.ConfigurationFactory.ConfigurationType()
}

// Default returns the manifest's default configuration without validating
// it, so callers that plan to apply further values (e.g. decoding CLI/spec
// input) can start from it before validation happens once, at the end.
func (m BaseManifest) Default() Configuration {
	if m.ConfigurationFactory == nil {
		return nil
	}
	return m.ConfigurationFactory.Default()
}

func (m BaseManifest) NewConfiguration() (Configuration, error) {
	if m.ConfigurationFactory == nil {
		return nil, fmt.Errorf("manifest %q has no configuration factory", m.Name)
	}
	config, err := m.ConfigurationFactory.New()
	if err != nil {
		return nil, err
	}
	configType, err := configurationType(config)
	if err != nil {
		return nil, err
	}
	if m.key.configType != nil && configType != m.key.configType {
		return nil, fmt.Errorf("manifest %q configuration factory returned %s, want %s", m.Name, configType, m.key.configType)
	}
	return config, nil
}

func (m BaseManifest) validate() error {
	if m.Name == "" {
		return fmt.Errorf("manifest name must not be empty")
	}
	if !isManifestName(m.Name) {
		return fmt.Errorf("manifest name %q must be lower kebab-case", m.Name)
	}
	return nil
}
