package host_test

import (
	"fmt"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/plugin"
)

type examplePluginID struct{}
type exampleCodecID struct{}
type exampleUnitID struct{}

type codecConfig struct{ Compression int }
type exampleUnit int

type exampleOperator struct{ shape flow.Shape }

func (o exampleOperator) Ports() flow.Shape { return o.shape.Clone() }
func (exampleOperator) Close() error        { return nil }

func codecSchema() config.Schema[codecConfig] {
	return config.Struct[codecConfig](func() codecConfig { return codecConfig{Compression: 5} }).
		Version("1").
		AddField(config.Field(
			"compression",
			func(c *codecConfig) *int { return &c.Compression },
			config.Int().Range(0, 8),
		)).
		Build()
}

// Codec is the shape a plugin family exposes: a function returning a
// definition, with no process-global registration.
func Codec() plugin.Definition {
	typ := schema.Define[exampleUnitID, exampleUnit](schema.Traits[exampleUnit]{})
	shape := flow.NewShape(nil, []flow.Port{flow.Out("output", typ)})
	spec := plugin.Spec[codecConfig, flow.Shape, int]{
		Shape: plugin.StaticShape[codecConfig](shape),
		Compile: func(plugin.CompileContext, codecConfig, flow.Descriptors[int]) (plugin.Compiled[flow.Shape, int], error) {
			return plugin.Compiled[flow.Shape, int]{Plan: shape, Outputs: flow.NewDescriptors(flow.Describe("output", 1))}, nil
		},
		Open: func(_ plugin.OpenContext, plan flow.Shape) (flow.Operator, error) {
			return exampleOperator{shape: plan}, nil
		},
	}
	return plugin.Define[examplePluginID](
		plugin.Descriptor{DisplayName: "Example", Version: "1.0.0", License: "MIT"},
		plugin.NewComponent[exampleCodecID](plugin.Descriptor{DisplayName: "Example codec"}, codecSchema(), plugin.WithSpec(spec)),
	)
}

// A Host owns one immutable catalog built from an explicit plugin set.
func ExampleNew() {
	h, err := host.New(host.Plugins(plugin.NewSet(Codec())))
	if err != nil {
		panic(err)
	}

	view, _ := h.Catalog().Lookup(plugin.IdentityOf[exampleCodecID]())
	fmt.Println(h.Catalog().Len())
	fmt.Println(view.Descriptor.DisplayName, view.Descriptor.Version)
	// Output:
	// 1
	// Example codec 1.0.0
}

// Composition problems are retained by the set and reported together when the
// host is built, so a broken composition can never disappear silently.
func ExampleNew_brokenComposition() {
	set := plugin.NewSet(Codec()).Add(Codec())

	if _, err := host.New(host.Plugins(set)); err != nil {
		for _, item := range host.Diagnostics(err) {
			fmt.Println(item.Code)
		}
	}
	// Output: plugin.duplicate-identity
}
