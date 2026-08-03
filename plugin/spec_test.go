package plugin

import (
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/schema"
)

type specUnitID struct{}
type specUnit struct{ Value int }

type specOperator struct{ shape flow.Shape }

func (o specOperator) Ports() flow.Shape { return o.shape }
func (o specOperator) Close() error      { return nil }

func TestComponentSpecValidatesPortsAndOpensOperator(t *testing.T) {
	typ := schema.Define[specUnitID, specUnit](schema.Traits[specUnit]{})
	shape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	component := NewComponent[specUnitID](Descriptor{DisplayName: "spec"}, pluginSchema(1), WithPorts(shape), WithOpen(func() (flow.Operator, error) {
		return specOperator{shape: shape}, nil
	}))
	if diagnostics := component.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("component diagnostics = %v", diagnostics)
	}
	operator, err := component.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := operator.Ports().Validate(); err != nil {
		t.Fatal(err)
	}
	view := component.View()
	view.Ports.Inputs[0] = flow.Out("mutated", typ)
	if component.Ports().Inputs[0].ID() != "in" {
		t.Fatal("component shape was mutated through its view")
	}
}

func TestComponentSpecRejectsInvalidShapeOrMissingOpen(t *testing.T) {
	component := NewComponent[specUnitID](Descriptor{DisplayName: "invalid"}, pluginSchema(1), WithPorts(flow.Shape{}))
	if len(component.Diagnostics()) == 0 {
		t.Fatal("invalid component spec unexpectedly accepted")
	}
}
