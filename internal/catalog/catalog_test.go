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
		AddField(config.Field("value", func(value *catalogConfig) *int { return &value.Value }, config.Int())).
		Build()
}

func catalogComponent[Marker any](name string) plugin.Component {
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: name, Version: "1.0.0"}, catalogSchema())
}

func TestBuildValidatesAndSortsImmutableIndex(t *testing.T) {
	first := catalogComponent[catalogFirstID]("first")
	second := catalogComponent[catalogSecondID]("second")
	definition := plugin.Define[catalogPluginID](plugin.Descriptor{DisplayName: "catalog", Version: "1.0.0"}, second, first)
	set, err := plugin.NewSet(definition)
	if err != nil {
		t.Fatalf("NewSet failed: %v", err)
	}
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
	set, err := plugin.NewSet(bad)
	if err != nil {
		t.Fatalf("NewSet failed: %v", err)
	}
	_, err = Build(set)
	if err == nil {
		t.Fatal("broken definition was accepted")
	}
	items := diagnostic.ItemsOf(err)
	if len(items) < 5 {
		t.Fatalf("got %d diagnostics, want aggregate: %v", len(items), err)
	}
	if len(diagnostic.ItemsOf(err)) == 0 {
		t.Fatal("catalog error is not structured")
	}
}
