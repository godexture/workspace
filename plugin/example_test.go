package plugin_test

import (
	"fmt"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/plugin"
)

type examplePluginID struct{}
type exampleComponentID struct{}
type exampleKeyID struct{}

type exampleConfig struct{ Level int }

func Codec() plugin.Definition {
	schema := config.Struct[exampleConfig](func() exampleConfig { return exampleConfig{Level: 5} }).
		Version("1").
		AddField(config.Field("level", func(value *exampleConfig) *int { return &value.Level }, config.Int().Range(0, 10))).
		Build()
	component := plugin.NewComponent[exampleComponentID](plugin.Descriptor{DisplayName: "Example codec", Version: "1.0.0"}, schema)
	return plugin.Define[examplePluginID](plugin.Descriptor{DisplayName: "Example plugin", Version: "1.0.0"}, component)
}

func ExampleNewSet() {
	set := plugin.NewSet(Codec())
	fmt.Println(len(set.Components()))
	// Output: 1
}

// Public vocabularies can opt into host-time validation. The key itself stays
// usable without registration, but one marker cannot mean two payload types
// in the same composition.
func ExampleDeclareKey() {
	text := key.Define[exampleKeyID, string]()
	number := key.Define[exampleKeyID, int]()
	set := plugin.NewSet().
		AddDeclaration(plugin.DeclareKey(text)).
		AddDeclaration(plugin.DeclareKey(number))

	_, err := host.New(host.Plugins(set))
	for _, item := range host.Diagnostics(err) {
		fmt.Println(item.Code)
	}
	// Output: catalog.declaration-conflict
}
