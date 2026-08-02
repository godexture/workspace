package host

import (
	"strings"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/plugin"
)

type hostPluginA struct{}
type hostPluginB struct{}
type hostComponentA struct{}
type hostComponentB struct{}

type hostConfig struct{ Value int }

func hostSchema(value int) config.Schema[hostConfig] {
	return config.Struct(func() hostConfig { return hostConfig{Value: value} }).
		Identity("host.test.config").
		Version("1").
		AddField(config.Field("value", func(value *hostConfig) *int { return &value.Value }, config.Int())).
		Build()
}

func hostDefinition[PluginMarker any, ComponentMarker any](name string, value int) plugin.Definition {
	component := plugin.NewComponent[ComponentMarker](plugin.Descriptor{DisplayName: name, Version: "1.0.0"}, hostSchema(value))
	return plugin.Define[PluginMarker](plugin.Descriptor{DisplayName: name + " plugin", Version: "1.0.0"}, component)
}

func TestNewCreatesIsolatedHostCatalogs(t *testing.T) {
	definitionA := hostDefinition[hostPluginA, hostComponentA]("a", 1)
	definitionB := hostDefinition[hostPluginB, hostComponentB]("b", 2)
	setA, err := plugin.NewSet(definitionA)
	if err != nil {
		t.Fatalf("set A: %v", err)
	}
	setB, err := plugin.NewSet(definitionB)
	if err != nil {
		t.Fatalf("set B: %v", err)
	}
	hostA, err := New(Plugins(setA))
	if err != nil {
		t.Fatalf("host A: %v", err)
	}
	hostB, err := New(Plugins(setB))
	if err != nil {
		t.Fatalf("host B: %v", err)
	}
	if hostA.Catalog().Len() != 1 || hostB.Catalog().Len() != 1 {
		t.Fatalf("catalog lengths = %d and %d", hostA.Catalog().Len(), hostB.Catalog().Len())
	}
	if _, ok := hostA.Catalog().Lookup(plugin.IdentityOf[hostComponentB]()); ok {
		t.Fatal("host A sees host B component")
	}
	if _, ok := hostB.Catalog().Lookup(plugin.IdentityOf[hostComponentA]()); ok {
		t.Fatal("host B sees host A component")
	}

	views := hostA.Catalog().Components()
	views[0].Schema.Fields[0].Help = "mutated"
	if hostA.Catalog().Components()[0].Schema.Fields[0].Help == "mutated" {
		t.Fatal("host catalog view is mutable")
	}
}

func TestNewReturnsAggregateIdentityAndFieldDiagnostics(t *testing.T) {
	badSchema := config.Struct(func() hostConfig { return hostConfig{} }).
		AddField(config.Field("value", func(value *hostConfig) *int { return &value.Value }, config.Int(), config.DependsOn("missing"))).
		AddField(config.Field("value", func(value *hostConfig) *int { return &value.Value }, config.Int())).
		Build()
	bad := plugin.Define[hostPluginA](plugin.Descriptor{}, plugin.NewComponent[hostComponentA](plugin.Descriptor{}, badSchema))
	set, err := plugin.NewSet(bad)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	_, err = New(Plugins(set))
	if err == nil {
		t.Fatal("invalid host unexpectedly constructed")
	}
	items := diagnostic.ItemsOf(err)
	if len(items) < 5 {
		t.Fatalf("got %d diagnostics, want aggregate: %v", len(items), err)
	}
	componentPath := plugin.IdentityOf[hostComponentA]().String()
	foundComponent := false
	foundField := false
	for _, item := range items {
		if strings.Contains(item.Path.String(), componentPath) {
			foundComponent = true
		}
		if strings.Contains(item.Path.String(), "value") {
			foundField = true
		}
	}
	if !foundComponent || !foundField {
		t.Fatalf("diagnostics lack component/field paths: %v", items)
	}
}
