package plugin_test

import (
	"fmt"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/plugin"
)

type examplePluginID struct{}
type exampleComponentID struct{}

type exampleConfig struct{ Level int }

func Codec() plugin.Definition {
	schema := config.Struct(func() exampleConfig { return exampleConfig{Level: 5} }).
		AddField(config.Field("level", func(value *exampleConfig) *int { return &value.Level }, config.Int().Range(0, 10))).
		Build()
	component := plugin.NewComponent[exampleComponentID](plugin.Descriptor{DisplayName: "Example codec", Version: "1.0.0"}, schema)
	return plugin.Define[examplePluginID](plugin.Descriptor{DisplayName: "Example plugin", Version: "1.0.0"}, component)
}

func ExampleNewSet() {
	set, err := plugin.NewSet(Codec())
	if err != nil {
		panic(err)
	}
	fmt.Println(len(set.Components()))
	// Output: 1
}
