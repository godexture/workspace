package endpoint

import (
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/plugin"
)

type endpointComponentID struct{}
type endpointPluginID struct{}
type endpointConfig struct{}
type endpointPayload struct{}
type endpointSchemaID struct{}

type endpointOperator struct{ shape flow.Shape }

func (o endpointOperator) Ports() flow.Shape { return o.shape }
func (o endpointOperator) Close() error      { return nil }

func TestEndpointTraitLayersOverNormalTypedComponentWithoutOpeningIt(t *testing.T) {
	typ := schema.Define[endpointSchemaID, endpointPayload](schema.Traits[endpointPayload]{})
	shape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	var opens atomic.Int32
	spec := plugin.Spec[endpointConfig, struct{}, int]{
		Ports: shape,
		Compile: func(plugin.CompileContext, endpointConfig, flow.Descriptors[int]) (plugin.Compiled[struct{}, int], error) {
			return plugin.Compiled[struct{}, int]{Outputs: flow.NewDescriptors(flow.Describe("out", 1))}, nil
		},
		Open: func(plugin.OpenContext, struct{}) (flow.Operator, error) {
			opens.Add(1)
			return endpointOperator{shape: shape}, nil
		},
	}
	trait, err := NewTrait(LiveDynamic, Realtime)
	if err != nil {
		t.Fatal(err)
	}
	component := plugin.NewComponent[endpointComponentID](
		plugin.Descriptor{DisplayName: "capture"},
		config.Struct[endpointConfig](func() endpointConfig { return endpointConfig{} }).Version("1").Build(),
		plugin.WithSpec(spec),
		WithTrait(trait),
	)
	attached, ok := TraitOf(component)
	if !ok || attached.Topology() != LiveDynamic || attached.Mode() != Realtime {
		t.Fatalf("endpoint trait = %#v, %v", attached, ok)
	}
	if len(component.Traits()) != 1 {
		t.Fatalf("component traits = %#v", component.Traits())
	}
	if opens.Load() != 0 {
		t.Fatal("endpoint construction opened component")
	}
}

func TestEndpointTraitRetainsInvalidValueForCompositionDiagnostics(t *testing.T) {
	component := plugin.NewComponent[endpointComponentID](
		plugin.Descriptor{DisplayName: "capture"},
		config.Struct[endpointConfig](func() endpointConfig { return endpointConfig{} }).Version("1").Build(),
		plugin.WithSpec(plugin.Spec[endpointConfig, struct{}, int]{}),
		WithTrait(Trait{}),
	)
	trait, ok := TraitOf(component)
	if !ok || trait.Valid() {
		t.Fatalf("invalid endpoint trait = %#v, %v", trait, ok)
	}
}

func TestDeviceQueryIsExplicitAndDeviceFieldsStaySeparate(t *testing.T) {
	component := plugin.IdentityOf[endpointComponentID]()
	reference, err := access.Parse("device:front")
	if err != nil {
		t.Fatal(err)
	}
	device, err := NewDevice(component, reference, DeviceDescriptor{Name: "Front camera"})
	if err != nil {
		t.Fatal(err)
	}
	query, err := NewDeviceQuery(component)
	if err != nil {
		t.Fatal(err)
	}
	if !device.Valid() || !query.Valid() || query.ComponentIdentity() != device.ComponentIdentity() || device.Descriptor().Name != "Front camera" {
		t.Fatalf("device/query = %#v, %#v", device, query)
	}
}
