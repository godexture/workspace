package plugin

import (
	"strings"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/schema"
)

type testPluginID struct{}
type secondPluginID struct{}
type testComponentID struct{}
type secondComponentID struct{}
type foundationUnitID struct{}

type pluginConfig struct {
	Level int
}

type foundationUnit int
type foundationOperator struct{ shape flow.Shape }

func (o foundationOperator) Ports() flow.Shape { return o.shape.Clone() }
func (foundationOperator) Close() error        { return nil }

func pluginSchema(defaultLevel int) config.Schema[pluginConfig] {
	return config.Struct[pluginConfig](func() pluginConfig { return pluginConfig{Level: defaultLevel} }).
		Version("1").
		AddField(config.Field("level", func(value *pluginConfig) *int { return &value.Level }, config.Int().Range(0, 10))).
		Build()
}

func pluginDescriptor(name string) Descriptor {
	return Descriptor{DisplayName: name, Version: "1.0.0", License: "MIT"}
}

func foundationComponent[Marker any](descriptor Descriptor, schemaValue config.Schema[pluginConfig], options ...ComponentOption) Component {
	typ := schema.Define[foundationUnitID, foundationUnit](schema.Traits[foundationUnit]{})
	shape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	spec := Spec[pluginConfig, flow.Shape, int]{
		Shape: StaticShape[pluginConfig](shape),
		Compile: func(CompileContext, pluginConfig, flow.Descriptors[int]) (Compiled[flow.Shape, int], error) {
			return Compiled[flow.Shape, int]{Plan: shape, Outputs: flow.NewDescriptors(flow.Describe("out", 1))}, nil
		},
		Open: func(_ OpenContext, plan flow.Shape) (flow.Operator, error) {
			return foundationOperator{shape: plan}, nil
		},
	}
	allOptions := append([]ComponentOption{WithSpec(spec)}, options...)
	return NewComponent[Marker](descriptor, schemaValue, allOptions...)
}

func TestIdentityUsesOnlyMarkerType(t *testing.T) {
	first := foundationComponent[testComponentID](pluginDescriptor("first"), pluginSchema(1), Aliases("one"))
	second := foundationComponent[testComponentID](pluginDescriptor("second"), pluginSchema(9), Aliases("two"))
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
	component := foundationComponent[testComponentID](pluginDescriptor("component"), pluginSchema(1))
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

func TestDefinitionInheritsPluginDescriptor(t *testing.T) {
	component := foundationComponent[testComponentID](Descriptor{DisplayName: "Component display"}, pluginSchema(1))
	markerFallback := foundationComponent[secondComponentID](Descriptor{}, pluginSchema(2))
	parent := Descriptor{
		DisplayName: "Plugin display",
		Homepage:    "https://example.com/plugin",
		Repository:  "https://example.com/repository",
		Version:     "2.0.0",
		License:     "MIT",
		Build:       BuildModePureGo,
		Digest:      "sha256:plugin",
		Signature:   "sig:plugin",
		Provenance:  Provenance{Revision: "revision", Builder: "builder"},
	}
	definition := Define[testPluginID](parent, component, markerFallback)
	components := definition.Components()
	got := components[0]
	expected := parent
	expected.DisplayName = "Component display"
	if got.Descriptor() != expected {
		t.Fatalf("component descriptor = %#v, want inherited %#v", got.Descriptor(), expected)
	}
	if got.Provenance() != parent.Provenance {
		t.Fatalf("component provenance = %#v, want inherited %#v", got.Provenance(), parent.Provenance)
	}
	fallback := components[1]
	fallbackExpected := parent
	fallbackExpected.DisplayName = fallback.Identity().Name()
	if fallback.Descriptor() != fallbackExpected {
		t.Fatalf("unset component descriptor = %#v, want inherited %#v", fallback.Descriptor(), fallbackExpected)
	}
	if got.Descriptor().DisplayName == fallback.Descriptor().DisplayName {
		t.Fatalf("same plugin components share display name: %#v", components)
	}
	views := definition.View().Components
	if views[0].Descriptor != expected || views[1].Descriptor != fallbackExpected {
		t.Fatalf("component view descriptors = %#v, want %#v and %#v", views[0].Descriptor, expected, fallbackExpected)
	}
	replaced := NewSet(definition).Override(component.Identity(), foundationComponent[testComponentID](Descriptor{}, pluginSchema(2)))
	replacementExpected := parent
	replacementExpected.DisplayName = component.Identity().Name()
	var replacement Component
	for _, candidate := range replaced.Components() {
		if candidate.Identity() == component.Identity() {
			replacement = candidate
			break
		}
	}
	if replacement.Descriptor() != replacementExpected {
		got := replacement.Descriptor()
		t.Fatalf("overridden component descriptor = %#v, want inherited %#v", got, replacementExpected)
	}

	standalone := foundationComponent[secondComponentID](Descriptor{}, pluginSchema(1))
	if got := standalone.View().Descriptor.DisplayName; got != standalone.Identity().Name() {
		t.Fatalf("standalone display name = %q, want marker name %q", got, standalone.Identity().Name())
	}
}

func TestZeroComponentViewIsSafe(t *testing.T) {
	component := Component{}
	view := component.View()
	if !view.Identity.IsZero() || view.Schema.Identity != "" {
		t.Fatalf("zero component view = %#v", view)
	}
	if len(component.Diagnostics()) == 0 {
		t.Fatal("zero component diagnostics are missing")
	}
}

func TestDescriptorBuildModeIsExclusive(t *testing.T) {
	if got := (Descriptor{DisplayName: "x", Version: "1", Build: BuildModeCGO}).Build.String(); got != "cgo" {
		t.Fatalf("build mode string = %q, want cgo", got)
	}
	items := (Descriptor{DisplayName: "x", Version: "1", Build: BuildMode(99)}).Validate()
	for _, item := range items {
		if item.Code == "plugin.descriptor.build-mode" {
			return
		}
	}
	t.Fatalf("invalid build mode diagnostic missing: %v", items)
}

func TestSetIsPersistentAndRejectsDuplicateMarkers(t *testing.T) {
	component := foundationComponent[testComponentID](pluginDescriptor("component"), pluginSchema(1))
	definition := Define[testPluginID](pluginDescriptor("plugin"), component)
	set := NewSet(definition)
	returned := set.Add(definition)
	if len(returned.Diagnostics()) == 0 {
		t.Fatal("duplicate plugin marker was not retained as a diagnostic")
	}
	if len(returned.Components()) != len(set.Components()) {
		t.Fatal("Add changed the returned receiver when retaining an error")
	}
	other := foundationComponent[secondComponentID](pluginDescriptor("other"), pluginSchema(2))
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
	component := foundationComponent[testComponentID](pluginDescriptor("old"), pluginSchema(1))
	definition := Define[testPluginID](pluginDescriptor("plugin"), component)
	set := NewSet(definition)
	replacement := foundationComponent[testComponentID](pluginDescriptor("new"), pluginSchema(8))
	overridden := set.Override(component.Identity(), replacement)
	if set.Components()[0].Descriptor().DisplayName != "old" {
		t.Fatalf("Override mutated original set")
	}
	if overridden.Components()[0].Descriptor().DisplayName != "new" {
		t.Fatalf("Override did not replace component")
	}
	missingComponent := foundationComponent[secondComponentID](pluginDescriptor("missing"), pluginSchema(9))
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
	badSchema := config.Struct[pluginConfig](func() pluginConfig { return pluginConfig{} }).
		AddField(config.Field("level", func(value *pluginConfig) *int { return &value.Level }, config.Int(), config.DependsOn("missing"))).
		AddField(config.Field("level", func(value *pluginConfig) *int { return &value.Level }, config.Int())).
		Build()
	component := foundationComponent[testComponentID](Descriptor{}, badSchema, Aliases("bad alias"))
	definition := Define[testPluginID](Descriptor{}, component)
	if len(definition.Diagnostics()) < 4 {
		t.Fatalf("got %d diagnostics, want aggregate: %v", len(definition.Diagnostics()), definition.Diagnostics())
	}
	if len(diagnostic.ItemsOf(diagnostic.NewError(definition.Diagnostics()...))) == 0 {
		t.Fatal("definition diagnostics are not structured")
	}
}

func TestMissingCompositionTargetSuggestsAlternatives(t *testing.T) {
	component := foundationComponent[testComponentID](pluginDescriptor("component"), pluginSchema(1))
	set := NewSet(Define[testPluginID](pluginDescriptor("plugin"), component))

	missing := IdentityOf[secondComponentID]()
	overridden := set.Override(missing, foundationComponent[secondComponentID](pluginDescriptor("other"), pluginSchema(2)))
	assertTargetDiagnostic(t, overridden.Diagnostics(), "plugin.override", missing, component.Identity())

	removed := set.Remove(missing)
	assertTargetDiagnostic(t, removed.Diagnostics(), "plugin.remove-missing", missing, component.Identity())
}

func assertTargetDiagnostic(t *testing.T, items []diagnostic.Item, code string, target, expected Identity) {
	t.Helper()
	for _, item := range items {
		if item.Code != code {
			continue
		}
		if item.Path.Component != target.String() {
			t.Fatalf("%s diagnostic does not name the target: %#v", code, item)
		}
		if !strings.Contains(item.Message, expected.String()) {
			t.Fatalf("%s diagnostic does not suggest %q: %q", code, expected, item.Message)
		}
		if item.Detail["suggestions"] == "" {
			t.Fatalf("%s diagnostic has no structured suggestions: %#v", code, item)
		}
		return
	}
	t.Fatalf("no %s diagnostic in %v", code, items)
}
