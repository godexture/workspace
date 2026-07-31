package config_test

import (
	"reflect"
	"testing"

	"github.com/godexture/core/registry"
	"github.com/godexture/sdk/config"
)

type dynamicConfig struct {
	Count int    `name:"count"`
	Items string `name:"items"`
}

func (c *dynamicConfig) ResolveConfiguration(context config.Context) ([]config.Field, error) {
	if !context.Explicit.Has("items") || context.Mode == config.Draft {
		c.Items = ""
		for index := 0; index < c.Count; index++ {
			if index > 0 {
				c.Items += ","
			}
			c.Items += "0"
		}
	}
	return []config.Field{{
		Name: "items", Active: true, DependsOn: []string{"count"},
		Slots: []config.Slot{{Index: 0, Label: "first", Default: 0}},
	}}, nil
}

func (c dynamicConfig) Validate() error { return nil }

type invalidDynamicConfig struct {
	Value string `name:"value"`
}

func (c *invalidDynamicConfig) ResolveConfiguration(config.Context) ([]config.Field, error) {
	return []config.Field{{Name: "missing", Active: true}}, nil
}

func (c invalidDynamicConfig) Validate() error { return nil }

type manifest struct {
	factory registry.ConfigurationFactory
}

func (m manifest) ID() registry.PluginKey                            { return registry.PluginKey{} }
func (m manifest) RegistryName() string                              { return "dynamic" }
func (m manifest) ConfigurationType() reflect.Type                   { return m.factory.ConfigurationType() }
func (m manifest) NewConfiguration() (registry.Configuration, error) { return m.factory.New() }
func (m manifest) Default() registry.Configuration                   { return m.factory.Default() }

func TestResolveTracksDynamicDefaultsAndNormalization(t *testing.T) {
	value := manifest{factory: registry.StaticConfigurationFactory(&dynamicConfig{Count: 2, Items: "0,0"})}

	_, defaulted, err := config.Resolve(value, map[string]string{"count": "3"}, config.Strict)
	if err != nil {
		t.Fatal(err)
	}
	if defaulted.Values["items"] != "0,0,0" || defaulted.Sources["items"] != config.SourceDynamic {
		t.Fatalf("dynamic items = %#v", defaulted)
	}

	_, normalized, err := config.Resolve(value, map[string]string{"count": "1", "items": "4,5"}, config.Draft)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Values["items"] != "0" || normalized.Updates["items"] != "0" || normalized.Sources["items"] != config.SourceNormalized {
		t.Fatalf("normalized items = %#v", normalized)
	}
}

func TestResolveRejectsUnknownDynamicField(t *testing.T) {
	value := manifest{factory: registry.StaticConfigurationFactory(&invalidDynamicConfig{})}
	if _, _, err := config.Resolve(value, nil, config.Strict); err == nil {
		t.Fatal("Resolve() accepted an unknown dynamic field")
	}
}
