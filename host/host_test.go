package host

import (
	"strings"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/plugin"
)

type hostPluginA struct{}
type hostPluginB struct{}
type hostComponentA struct{}
type hostComponentB struct{}
type hostSharedKey struct{}
type hostSecondKey struct{}
type hostUnitID struct{}

type hostConfig struct{ Value int }
type hostUnit int

type hostOperator struct{ shape flow.Shape }

func (o hostOperator) Ports() flow.Shape { return o.shape.Clone() }
func (hostOperator) Close() error        { return nil }

func hostSchema(value int) config.Schema[hostConfig] {
	return config.Struct[hostConfig](func() hostConfig { return hostConfig{Value: value} }).
		Version("1").
		AddField(config.Field("value", func(value *hostConfig) *int { return &value.Value }, config.Int())).
		Build()
}

func hostDefinition[PluginMarker any, ComponentMarker any](name string, value int) plugin.Definition {
	component := hostComponent[ComponentMarker](plugin.Descriptor{DisplayName: name, Version: "1.0.0"}, hostSchema(value))
	return plugin.Define[PluginMarker](plugin.Descriptor{DisplayName: name + " plugin", Version: "1.0.0"}, component)
}

func hostComponent[Marker any](descriptor plugin.Descriptor, schemaValue config.Schema[hostConfig]) plugin.Component {
	typ := schema.Define[hostUnitID, hostUnit](schema.Traits[hostUnit]{})
	shape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	spec := plugin.Spec[hostConfig, flow.Shape, int]{
		Ports: shape,
		Compile: func(plugin.CompileContext, hostConfig, flow.Descriptors[int]) (plugin.Compiled[flow.Shape, int], error) {
			return plugin.Compiled[flow.Shape, int]{Plan: shape, Outputs: flow.NewDescriptors(flow.Describe("out", 1))}, nil
		},
		Open: func(_ plugin.OpenContext, plan flow.Shape) (flow.Operator, error) {
			return hostOperator{shape: plan}, nil
		},
	}
	return plugin.NewComponent[Marker](descriptor, schemaValue, plugin.WithSpec(spec))
}

func TestNewCreatesIsolatedHostCatalogs(t *testing.T) {
	definitionA := hostDefinition[hostPluginA, hostComponentA]("a", 1)
	definitionB := hostDefinition[hostPluginB, hostComponentB]("b", 2)
	setA := plugin.NewSet(definitionA)
	setB := plugin.NewSet(definitionB)
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

func TestCatalogFingerprintTracksCompositionAndSurface(t *testing.T) {
	definition := hostDefinition[hostPluginA, hostComponentA]("a", 1)
	first, err := New(Plugins(plugin.NewSet(definition)))
	if err != nil {
		t.Fatalf("first host: %v", err)
	}
	second, err := New(Plugins(plugin.NewSet(definition)))
	if err != nil {
		t.Fatalf("second host: %v", err)
	}
	if first.Catalog().Fingerprint().IsZero() {
		t.Fatal("catalog fingerprint is zero")
	}
	if first.Catalog().Fingerprint() != second.Catalog().Fingerprint() {
		t.Fatalf("same composition produced different fingerprints: %s vs %s", first.Catalog().Fingerprint(), second.Catalog().Fingerprint())
	}

	changed := plugin.Define[hostPluginA](plugin.Descriptor{DisplayName: "a plugin", Version: "2.0.0"},
		hostComponent[hostComponentA](plugin.Descriptor{DisplayName: "a", Version: "2.0.0"}, hostSchema(1)))
	third, err := New(Plugins(plugin.NewSet(changed)))
	if err != nil {
		t.Fatalf("changed host: %v", err)
	}
	if first.Catalog().Fingerprint() == third.Catalog().Fingerprint() {
		t.Fatal("surface descriptor change did not change catalog fingerprint")
	}

	pluginOnlyChange := plugin.Define[hostPluginA](plugin.Descriptor{DisplayName: "a plugin", Version: "2.0.0"},
		hostComponent[hostComponentA](plugin.Descriptor{DisplayName: "a", Version: "1.0.0"}, hostSchema(1)))
	fourth, err := New(Plugins(plugin.NewSet(pluginOnlyChange)))
	if err != nil {
		t.Fatalf("plugin-only changed host: %v", err)
	}
	if first.Catalog().Fingerprint() == fourth.Catalog().Fingerprint() {
		t.Fatal("plugin descriptor change did not change catalog fingerprint")
	}
}

func TestNewReturnsAggregateIdentityAndFieldDiagnostics(t *testing.T) {
	badSchema := config.Struct[hostConfig](func() hostConfig { return hostConfig{} }).
		AddField(config.Field("value", func(value *hostConfig) *int { return &value.Value }, config.Int(), config.DependsOn("missing"))).
		AddField(config.Field("value", func(value *hostConfig) *int { return &value.Value }, config.Int())).
		Build()
	bad := plugin.Define[hostPluginA](plugin.Descriptor{}, hostComponent[hostComponentA](plugin.Descriptor{}, badSchema))
	set := plugin.NewSet(bad)
	_, err := New(Plugins(set))
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

func TestNewRejectsRetainedSetCompositionDiagnostics(t *testing.T) {
	first := hostDefinition[hostPluginA, hostComponentA]("first", 1)
	duplicate := hostDefinition[hostPluginA, hostComponentB]("duplicate", 2)
	set := plugin.NewSet(first).Add(duplicate)

	_, err := New(Plugins(set))
	if err == nil {
		t.Fatal("host accepted a set with a duplicate plugin identity")
	}
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "plugin.duplicate-identity" {
			return
		}
	}
	t.Fatalf("duplicate set diagnostic was not aggregated: %v", err)
}

func TestNewRejectsComponentWithZeroSchema(t *testing.T) {
	bad := plugin.Define[hostPluginB](plugin.Descriptor{DisplayName: "broken", Version: "1.0.0"},
		hostComponent[hostComponentB](plugin.Descriptor{DisplayName: "zero schema", Version: "1.0.0"}, config.Schema[hostConfig]{}))
	set := plugin.NewSet(bad)
	_, err := New(Plugins(set))
	if err == nil {
		t.Fatal("host accepted a component with an unbuilt zero schema")
	}
	componentID := plugin.IdentityOf[hostComponentB]().String()
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "config.invalid-schema" && strings.Contains(item.Path.String(), componentID) {
			return
		}
	}
	t.Fatalf("zero schema diagnostic lacks component identity: %v", err)
}

func TestNewAcceptsEquivalentKeyDeclarationsAcrossContainers(t *testing.T) {
	shared := key.Define[hostSharedKey, string]()
	streamProperty := property.Define[hostSharedKey, string](func(value string) ([]byte, error) {
		return []byte(value), nil
	})
	set := plugin.NewSet().
		AddDeclaration(plugin.DeclareKey(shared)).
		AddDeclaration(plugin.DeclareKey(streamProperty))

	instance, err := New(Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	declarations := instance.Catalog().Declarations()
	if len(declarations) != 1 {
		t.Fatalf("equivalent key declarations were not normalized: %#v", declarations)
	}
	targets := declarations[0].Targets()
	if len(targets) != 1 {
		t.Fatalf("key declaration targets = %#v", targets)
	}
	valueType, typeTarget := targets[0].Type()
	if !typeTarget || valueType != shared.ValueType() {
		t.Fatalf("key declaration target = %#v", targets)
	}
	single, err := New(Plugins(plugin.NewSet().AddDeclaration(plugin.DeclareKey(shared))))
	if err != nil {
		t.Fatal(err)
	}
	if instance.Catalog().Fingerprint() != single.Catalog().Fingerprint() {
		t.Fatal("an equivalent duplicate key declaration changed the catalog fingerprint")
	}
}

func TestNewRejectsConflictingKeyPayloadTypes(t *testing.T) {
	tests := map[string]plugin.Set{
		"shared key container": plugin.NewSet().
			AddDeclaration(plugin.DeclareKey(key.Define[hostSharedKey, string]())).
			AddDeclaration(plugin.DeclareKey(key.Define[hostSharedKey, int64]())),
		"key and property containers": plugin.NewSet().
			AddDeclaration(plugin.DeclareKey(key.Define[hostSecondKey, string]())).
			AddDeclaration(plugin.DeclareKey(property.Define[hostSecondKey, int64](func(value int64) ([]byte, error) {
				return []byte{byte(value)}, nil
			}))),
	}
	for name, set := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := New(Plugins(set))
			if err == nil {
				t.Fatal("conflicting key payload types were accepted")
			}
			for _, item := range Diagnostics(err) {
				if item.Code == "catalog.declaration-conflict" {
					return
				}
			}
			t.Fatalf("key declaration conflict diagnostic missing: %v", err)
		})
	}
}

func TestNewPreservesInvalidKeyDeclarationProblem(t *testing.T) {
	invalid := property.Define[hostSharedKey, string](nil)
	_, err := New(Plugins(plugin.NewSet().AddDeclaration(plugin.DeclareKey(invalid))))
	if err == nil {
		t.Fatal("invalid property declaration was accepted")
	}
	for _, item := range Diagnostics(err) {
		if item.Code == "catalog.invalid-declaration" && strings.Contains(item.Message, "canonical encoder") {
			return
		}
	}
	t.Fatalf("invalid key problem was not preserved: %v", err)
}
