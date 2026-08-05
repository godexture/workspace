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
	component := plugin.NewComponent[endpointComponentID](plugin.Descriptor{DisplayName: "capture"}, config.Struct[endpointConfig](func() endpointConfig { return endpointConfig{} }).Build(), plugin.WithPorts(shape), plugin.WithOpen(func() (flow.Operator, error) {
		opens.Add(1)
		return endpointOperator{shape: shape}, nil
	}))
	trait, err := NewTrait(LiveDynamic, Realtime)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := New(component, trait)
	if err != nil {
		t.Fatal(err)
	}
	if !endpoint.Valid() || endpoint.Identity() != component.Identity() || endpoint.Trait().Topology() != LiveDynamic {
		t.Fatalf("endpoint = %#v", endpoint)
	}
	if opens.Load() != 0 {
		t.Fatal("endpoint construction opened component")
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
