package catalog

import (
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/plugin"
)

type catalogPluginID struct{}
type catalogFirstID struct{}
type catalogSecondID struct{}

type catalogConfig struct{ Value int }

func catalogSchema() config.Schema[catalogConfig] {
	return config.Struct(func() catalogConfig { return catalogConfig{Value: 1} }).
		Identity("catalog.test.config").
		Version("1").
		AddField(config.Field("value", func(value *catalogConfig) *int { return &value.Value }, config.Int().Range(0, 10))).
		Build()
}

func catalogComponent[Marker any](name string) plugin.Component {
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: name, Version: "1.0.0"}, catalogSchema())
}

func TestBuildValidatesAndSortsImmutableIndex(t *testing.T) {
	first := catalogComponent[catalogFirstID]("first")
	second := catalogComponent[catalogSecondID]("second")
	definition := plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1.0.0"}, second, first)
	set := plugin.NewSet(definition)
	index, err := Build(set)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if index.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", index.Len())
	}
	components := index.Components()
	if components[0].Identity().String() >= components[1].Identity().String() {
		t.Fatalf("components are not sorted by identity")
	}
	views := index.Views()
	views[0].Aliases = append(views[0].Aliases, "mutated")
	if len(index.Views()[0].Aliases) != 0 {
		t.Fatalf("catalog view aliases are mutable")
	}
}

func TestBuildRejectsBrokenDefinitionWithoutDroppingErrors(t *testing.T) {
	badSchema := config.Struct(func() catalogConfig { return catalogConfig{} }).
		AddField(config.Field("value", func(value *catalogConfig) *int { return &value.Value }, config.Int(), config.DependsOn("unknown"))).
		AddField(config.Field("value", func(value *catalogConfig) *int { return &value.Value }, config.Int())).
		Build()
	bad := plugin.Define[catalogPluginID](plugin.Descriptor{}, plugin.NewComponent[catalogFirstID](plugin.Descriptor{}, badSchema))
	set := plugin.NewSet(bad)
	_, err := Build(set)
	if err == nil {
		t.Fatal("broken definition was accepted")
	}
	items := diagnostic.ItemsOf(err)
	if len(items) < 5 {
		t.Fatalf("got %d diagnostics, want aggregate: %v", len(items), err)
	}
}

func TestBuildIncludesSetCompositionDiagnostics(t *testing.T) {
	first := plugin.Define[catalogPluginID](
		plugin.Descriptor{DisplayName: "catalog", Version: "1.0.0"},
		catalogComponent[catalogFirstID]("first"),
	)
	duplicate := plugin.Define[catalogPluginID](
		plugin.Descriptor{DisplayName: "catalog replacement", Version: "1.0.0"},
		catalogComponent[catalogSecondID]("second"),
	)
	set := plugin.NewSet(first).Add(duplicate)

	_, err := Build(set)
	if err == nil {
		t.Fatal("Build accepted retained set composition diagnostics")
	}
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "plugin.duplicate-identity" {
			return
		}
	}
	t.Fatalf("set composition diagnostic was not aggregated: %v", err)
}

func TestCatalogComponentResolvesPatch(t *testing.T) {
	component := catalogComponent[catalogFirstID]("first")
	definition := plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1.0.0"}, component)
	index, err := Build(plugin.NewSet(definition))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	fromCatalog, ok := index.Lookup(component.Identity())
	if !ok {
		t.Fatal("catalog component lookup failed")
	}
	resolved, err := fromCatalog.Resolve(config.NewPatch().SetText("value", "7"))
	if err != nil {
		t.Fatalf("catalog component resolve failed: %v", err)
	}
	value, ok := resolved.Value.(catalogConfig)
	if !ok || value.Value != 7 {
		t.Fatalf("resolved catalog value = %#v, want catalogConfig{Value: 7}", resolved.Value)
	}

	_, err = fromCatalog.Resolve(config.NewPatch().SetText("value", "99").SetText("unknown", "1"))
	if err == nil {
		t.Fatal("invalid catalog patch unexpectedly resolved")
	}
	paths := make(map[string]bool)
	for _, item := range diagnostic.ItemsOf(err) {
		paths[item.Path.String()] = true
	}
	if !paths["value"] || !paths["unknown"] {
		t.Fatalf("catalog resolver diagnostics lack field paths: %v", err)
	}
}

func TestBuildRejectsMarkerSharedByPluginAndComponent(t *testing.T) {
	definition := plugin.Define[catalogPluginID](
		plugin.Descriptor{DisplayName: "catalog", Version: "1.0.0"},
		catalogComponent[catalogPluginID]("shared"),
	)
	_, err := Build(plugin.NewSet(definition))
	if err == nil {
		t.Fatal("a marker used by both a plugin and a component was accepted")
	}
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "catalog.identity-conflict" {
			return
		}
	}
	t.Fatalf("identity conflict diagnostic missing: %v", err)
}
