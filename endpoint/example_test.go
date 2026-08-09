package endpoint_test

import (
	"fmt"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/plugin"
)

type endpointExampleComponentID struct{}
type endpointExampleConfig struct{}
type endpointExampleSchemaID struct{}
type endpointExampleUnit int

type endpointExampleOperator struct{ shape flow.Shape }

func (o endpointExampleOperator) Ports() flow.Shape { return o.shape.Clone() }
func (endpointExampleOperator) Close() error        { return nil }

// Traits describe endpoint behavior without scanning or opening a device.
func ExampleNewTrait() {
	trait, err := endpoint.NewTrait(endpoint.LiveDynamic, endpoint.Realtime)
	if err != nil {
		panic(err)
	}

	fmt.Println(trait.Valid())
	fmt.Println(trait.Topology() == endpoint.LiveDynamic, trait.Mode() == endpoint.Realtime)
	// Output:
	// true
	// true true
}

// Endpoint behavior travels with the component inside plugin.Set; Host does
// not need a separate endpoint option.
func ExampleWithTrait() {
	typ := schema.Define[endpointExampleSchemaID, endpointExampleUnit](schema.Traits[endpointExampleUnit]{})
	shape := flow.NewShape(nil, []flow.Port{flow.Out("frames", typ)})
	trait, _ := endpoint.NewTrait(endpoint.LiveStatic, endpoint.Realtime)
	component := plugin.NewComponent[endpointExampleComponentID](
		plugin.Descriptor{DisplayName: "capture", Version: "1"},
		config.Struct[endpointExampleConfig](func() endpointExampleConfig { return endpointExampleConfig{} }).Version("1").Build(),
		plugin.WithSpec(plugin.Spec[endpointExampleConfig, flow.Shape, int]{
			Shape: plugin.StaticShape[endpointExampleConfig](shape),
			Compile: func(plugin.CompileContext, endpointExampleConfig, flow.Descriptors[int]) (plugin.Compiled[flow.Shape, int], error) {
				return plugin.Compiled[flow.Shape, int]{Plan: shape, Outputs: flow.NewDescriptors(flow.Describe("frames", 1))}, nil
			},
			Open: func(plugin.OpenContext, flow.Shape) (flow.Operator, error) {
				return endpointExampleOperator{shape: shape}, nil
			},
		}),
		plugin.WithReader("frames", typ),
		endpoint.WithTrait(trait),
	)
	attached, ok := endpoint.TraitOf(component)

	fmt.Println(ok, attached.Topology(), attached.Mode())
	fmt.Println(component.View().Executable, len(component.Traits()))
	// Output:
	// true live-static realtime
	// true 1
}
