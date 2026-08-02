package plugin

import (
	"strings"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
)

type testPluginID struct{}
type secondPluginID struct{}
type testComponentID struct{}
type secondComponentID struct{}

type pluginConfig struct {
	Level int
}

func pluginSchema(defaultLevel int) config.Schema[pluginConfig] {
	return config.Struct(func() pluginConfig { return pluginConfig{Level: defaultLevel} }).
		Identity("plugin.test.config").
		Version("1").
		AddField(config.Field("level", func(value *pluginConfig) *int { return &value.Level }, config.Int().Range(0, 10))).
		Build()
}

func pluginDescriptor(name string) Descriptor {
	return Descriptor{DisplayName: name, Version: "1.0.0", License: "MIT"}
}

func TestIdentityUsesOnlyMarkerType(t *testing.T) {
	first := NewComponent[testComponentID](pluginDescriptor("first"), pluginSchema(1), Aliases("one"))
	second := NewComponent[testComponentID](pluginDescriptor("second"), pluginSchema(9), Aliases("two"))
	if first.Identity() != second.Identity() {
		t.Fatalf("same marker produced different identities: %q vs %q", first.Identity(), second.Identity())
	}
	if first.Identity() != IdentityOf[testComponentID]() {
		t.Fatalf("component identity does not match marker identity")
	}
	if strings.Contains(first.Identity().String(), "1.0.0") || strings.Contains(first.Identity().String(), "plugin.test.config") {
		t.Fatalf("identity contains implementation metadata: %q", first.Identity())
	}
}

func TestSetIsPersistentAndRejectsDuplicateMarkers(t *testing.T) {
	component := NewComponent[testComponentID](pluginDescriptor("component"), pluginSchema(1))
	definition := Define[testPluginID](pluginDescriptor("plugin"), component)
	set, err := NewSet(definition)
	if err != nil {
		t.Fatalf("NewSet failed: %v", err)
	}
	returned, err := set.Add(definition)
	if err == nil {
		t.Fatal("duplicate plugin marker was accepted")
	}
	if len(returned.Components()) != len(set.Components()) {
		t.Fatal("Add changed the returned receiver on error")
	}
	other := NewComponent[secondComponentID](pluginDescriptor("other"), pluginSchema(2))
	otherDefinition := Define[secondPluginID](pluginDescriptor("other-plugin"), other)
	withOther, err := set.Add(otherDefinition)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if len(set.Components()) != 1 || len(withOther.Components()) != 2 {
		t.Fatalf("persistent set mutated: original=%d new=%d", len(set.Components()), len(withOther.Components()))
	}
}

func TestOverrideAndRemoveDoNotMutateOriginal(t *testing.T) {
	component := NewComponent[testComponentID](pluginDescriptor("old"), pluginSchema(1))
	definition := Define[testPluginID](pluginDescriptor("plugin"), component)
	set, err := NewSet(definition)
	if err != nil {
		t.Fatalf("NewSet failed: %v", err)
	}
	replacement := NewComponent[testComponentID](pluginDescriptor("new"), pluginSchema(8))
	overridden, err := set.Override(component.Identity(), replacement)
	if err != nil {
		t.Fatalf("Override failed: %v", err)
	}
	if set.Components()[0].Descriptor().DisplayName != "old" {
		t.Fatalf("Override mutated original set")
	}
	if overridden.Components()[0].Descriptor().DisplayName != "new" {
		t.Fatalf("Override did not replace component")
	}
	removed, ok := overridden.Remove(component.Identity())
	if !ok || !removed.Empty() {
		t.Fatalf("Remove = %#v, %v; want empty set", removed, ok)
	}
	if overridden.Empty() {
		t.Fatal("Remove mutated source set")
	}
}

func TestInvalidComponentKeepsAggregateDiagnostics(t *testing.T) {
	badSchema := config.Struct(func() pluginConfig { return pluginConfig{} }).
		AddField(config.Field("level", func(value *pluginConfig) *int { return &value.Level }, config.Int(), config.DependsOn("missing"))).
		AddField(config.Field("level", func(value *pluginConfig) *int { return &value.Level }, config.Int())).
		Build()
	component := NewComponent[testComponentID](Descriptor{}, badSchema, Aliases("bad alias"))
	definition := Define[testPluginID](Descriptor{}, component)
	if len(definition.Diagnostics()) < 4 {
		t.Fatalf("got %d diagnostics, want aggregate: %v", len(definition.Diagnostics()), definition.Diagnostics())
	}
	if len(diagnostic.ItemsOf(diagnostic.NewError(definition.Diagnostics()...))) == 0 {
		t.Fatal("definition diagnostics are not structured")
	}
}
