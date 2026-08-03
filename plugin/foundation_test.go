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

func TestComponentResolvesTypeErasedPatch(t *testing.T) {
	component := NewComponent[testComponentID](pluginDescriptor("component"), pluginSchema(1))
	resolved, err := component.Resolve(config.NewPatch().SetText("level", "7"))
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	value, ok := resolved.Value.(pluginConfig)
	if !ok || value.Level != 7 {
		t.Fatalf("resolved value = %#v, want pluginConfig{Level: 7}", resolved.Value)
	}

	_, err = component.Resolve(config.NewPatch().SetText("level", "99").SetText("unknown", "1"))
	if err == nil {
		t.Fatal("invalid patch unexpectedly resolved")
	}
	items := diagnostic.ItemsOf(err)
	paths := make(map[string]bool, len(items))
	for _, item := range items {
		paths[item.Path.String()] = true
	}
	if !paths["level"] || !paths["unknown"] {
		t.Fatalf("resolver diagnostics lack field paths: %v", items)
	}
}

func TestSetIsPersistentAndRejectsDuplicateMarkers(t *testing.T) {
	component := NewComponent[testComponentID](pluginDescriptor("component"), pluginSchema(1))
	definition := Define[testPluginID](pluginDescriptor("plugin"), component)
	set := NewSet(definition)
	returned := set.Add(definition)
	if len(returned.Diagnostics()) == 0 {
		t.Fatal("duplicate plugin marker was not retained as a diagnostic")
	}
	if len(returned.Components()) != len(set.Components()) {
		t.Fatal("Add changed the returned receiver when retaining an error")
	}
	other := NewComponent[secondComponentID](pluginDescriptor("other"), pluginSchema(2))
	otherDefinition := Define[secondPluginID](pluginDescriptor("other-plugin"), other)
	withOther := set.Add(otherDefinition)
	if len(set.Components()) != 1 || len(withOther.Components()) != 2 {
		t.Fatalf("persistent set mutated: original=%d new=%d", len(set.Components()), len(withOther.Components()))
	}
	continued := returned.Add(otherDefinition)
	if len(continued.Diagnostics()) != len(returned.Diagnostics()) {
		t.Fatalf("composition diagnostic was not retained: before=%v after=%v", returned.Diagnostics(), continued.Diagnostics())
	}
}

func TestOverrideAndRemoveDoNotMutateOriginal(t *testing.T) {
	component := NewComponent[testComponentID](pluginDescriptor("old"), pluginSchema(1))
	definition := Define[testPluginID](pluginDescriptor("plugin"), component)
	set := NewSet(definition)
	replacement := NewComponent[testComponentID](pluginDescriptor("new"), pluginSchema(8))
	overridden := set.Override(component.Identity(), replacement)
	if set.Components()[0].Descriptor().DisplayName != "old" {
		t.Fatalf("Override mutated original set")
	}
	if overridden.Components()[0].Descriptor().DisplayName != "new" {
		t.Fatalf("Override did not replace component")
	}
	missingComponent := NewComponent[secondComponentID](pluginDescriptor("missing"), pluginSchema(9))
	missing := set.Override(missingComponent.Identity(), missingComponent)
	if len(missing.Diagnostics()) == 0 || len(missing.Components()) != len(set.Components()) {
		t.Fatalf("Override diagnostic returned the wrong receiver: result=%d source=%d diagnostics=%v", len(missing.Components()), len(set.Components()), missing.Diagnostics())
	}
	missingPlugin := Define[secondPluginID](pluginDescriptor("missing-plugin"), missingComponent)
	missing = set.OverridePlugin(missingPlugin.Identity(), missingPlugin)
	if len(missing.Diagnostics()) == 0 || len(missing.Components()) != len(set.Components()) {
		t.Fatalf("OverridePlugin diagnostic returned the wrong receiver: result=%d source=%d diagnostics=%v", len(missing.Components()), len(set.Components()), missing.Diagnostics())
	}
	removed := overridden.Remove(component.Identity())
	if !removed.Empty() {
		t.Fatalf("Remove = %#v; want empty set", removed)
	}
	if overridden.Empty() {
		t.Fatal("Remove mutated source set")
	}
	missingRemoval := set.Remove(IdentityOf[secondComponentID]())
	if len(missingRemoval.Diagnostics()) == 0 {
		t.Fatal("missing Remove target was not retained as a diagnostic")
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
