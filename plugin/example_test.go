package plugin_test

import (
	"context"
	"fmt"
	"io"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/media/key"
	mediaSchema "github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/plugin"
)

type examplePluginID struct{}
type exampleComponentID struct{}
type exampleKeyID struct{}
type exampleUnitID struct{}

type exampleConfig struct{ Level int }
type exampleUnit int

type exampleOperator struct{ shape flow.Shape }

func (o exampleOperator) Ports() flow.Shape { return o.shape.Clone() }
func (exampleOperator) Close() error        { return nil }
func (exampleOperator) Read(context.Context) (flow.Input[exampleUnit], error) {
	return flow.Input[exampleUnit]{}, io.EOF
}

func Codec() plugin.Definition {
	schema := config.Struct[exampleConfig](func() exampleConfig { return exampleConfig{Level: 5} }).
		Version("1").
		AddField(config.Field("level", func(value *exampleConfig) *int { return &value.Level }, config.Int().Range(0, 10))).
		Build()
	typ := mediaSchema.Define[exampleUnitID, exampleUnit](mediaSchema.Traits[exampleUnit]{})
	shape := flow.NewShape(nil, []flow.Port{flow.Out("output", typ)})
	spec := plugin.Spec[exampleConfig, flow.Shape, int]{
		Shape: plugin.StaticShape[exampleConfig](shape),
		Compile: func(_ plugin.CompileContext, value exampleConfig, _ flow.Descriptors[int]) (plugin.Compiled[flow.Shape, int], error) {
			return plugin.Compiled[flow.Shape, int]{Plan: shape, Outputs: flow.NewDescriptors(flow.Describe("output", value.Level))}, nil
		},
		Open: func(_ plugin.OpenContext, plan flow.Shape) (flow.Operator, error) {
			return exampleOperator{shape: plan}, nil
		},
	}
	component := plugin.NewComponent[exampleComponentID](
		plugin.Descriptor{DisplayName: "Example codec", Version: "1.0.0"},
		schema,
		plugin.WithSpec(spec),
		plugin.WithReader("output", typ),
	)
	return plugin.Define[examplePluginID](plugin.Descriptor{DisplayName: "Example plugin", Version: "1.0.0"}, component)
}

func ExampleNewSet() {
	set := plugin.NewSet(Codec())
	fmt.Println(len(set.Components()))
	// Output: 1
}

// Typed execution registration captures T once without exposing a queue or
// scheduler to the component implementation.
func ExampleWithReader() {
	component := Codec().Components()[0]
	fmt.Println(component.View().Executable, component.Ports().Outputs[0].Schema().Payload().Name())
	// Output: true exampleUnit
}

// Compile resolves dynamic semantics without opening a runtime operator. The
// private plan can only be consumed later by the component that created it.
func ExampleCompile() {
	component := Codec().Components()[0]
	resolved, err := component.Resolve(config.NewPatch().Set("level", 7))
	if err != nil {
		panic(err)
	}
	compiled, err := plugin.Compile(component, plugin.CompileContext{}, resolved, flow.NewDescriptors[int]())
	if err != nil {
		panic(err)
	}
	outputs, _ := plugin.OutputsOf[int](compiled)
	value, _ := outputs.One("output")
	fmt.Println(value, compiled.Valid())
	// Output: 7 true
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
