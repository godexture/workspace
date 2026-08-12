package host

import (
	"strings"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type (
	formatSelectionPluginID struct{}
	formatSelectionConfigID struct{}
	formatSelectionFirstID  struct{}
	formatSelectionSecondID struct{}
	formatSelectionUnitID   struct{}
	formatSelectionFirst    struct{}
	formatSelectionSecond   struct{}
	formatSelectionConfig   struct{}
	formatSelectionUnit     int
	formatSelectionOperator struct{ shape flow.Shape }
)

func (o formatSelectionOperator) Ports() flow.Shape { return o.shape.Clone() }
func (formatSelectionOperator) Close() error        { return nil }

func TestResolveWriteFormatReportsOrderIndependentExtensionAmbiguity(t *testing.T) {
	firstFormat, _ := mediaformat.Define[formatSelectionFirst](nil, mediaformat.WithExtensions("shared"))
	secondFormat, _ := mediaformat.Define[formatSelectionSecond](nil, mediaformat.WithExtensions("shared"))
	first := formatSelectionComponent[formatSelectionFirstID](firstFormat)
	second := formatSelectionComponent[formatSelectionSecondID](secondFormat)
	extension, _ := mediaformat.ParseExtension("shared")
	selector, _ := job.SelectFormatExtension(extension)
	boundary := plan.Boundary{Node: "output", Scheme: "file", Direction: plan.OutputBoundary}

	var previous string
	for _, components := range [][]plugin.Component{{first, second}, {second, first}} {
		index, err := catalog.Build(plugin.NewSet(plugin.Define[formatSelectionPluginID](
			plugin.Descriptor{DisplayName: "Format selection fixture", Version: "1"},
			components...,
		)))
		if err != nil {
			t.Fatal(err)
		}
		_, err = (&Host{index: index}).resolveWriteFormat(boundary, selector)
		items := diagnostic.ItemsOf(err)
		if len(items) != 1 || items[0].Code != "prepare.format-ambiguous" || items[0].Detail["direction"] != "write" || !strings.Contains(items[0].Detail["candidates"], first.Identity().String()) || !strings.Contains(items[0].Detail["candidates"], second.Identity().String()) {
			t.Fatalf("write Format ambiguity = %#v, %v", items, err)
		}
		if previous != "" && items[0].Detail["candidates"] != previous {
			t.Fatalf("candidate order changed: %q / %q", previous, items[0].Detail["candidates"])
		}
		previous = items[0].Detail["candidates"]
	}
}

func formatSelectionComponent[Marker any](value mediaformat.Format) plugin.Component {
	typ := schema.Define[formatSelectionUnitID, formatSelectionUnit](schema.Traits[formatSelectionUnit]{})
	shape := flow.NewShape(
		[]flow.Port{flow.In("in", typ)},
		[]flow.Port{flow.Out("writes", access.Writes())},
	)
	spec := plugin.Spec[formatSelectionConfig, flow.Shape, int]{
		Shape: plugin.StaticShape[formatSelectionConfig](shape),
		Compile: func(plugin.CompileContext, formatSelectionConfig, flow.Descriptors[int]) (plugin.Compiled[flow.Shape, int], error) {
			return plugin.Compiled[flow.Shape, int]{Plan: shape, Outputs: flow.NewDescriptors(flow.Describe("writes", 1))}, nil
		},
		Open: func(plugin.OpenContext, flow.Shape) (flow.Operator, error) {
			return formatSelectionOperator{shape: shape}, nil
		},
	}
	configuration := config.Struct[formatSelectionConfigID](func() formatSelectionConfig { return formatSelectionConfig{} }).Version("1").Build()
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: "Format writer"}, configuration,
		plugin.WithSpec(spec),
		mediaformat.Write(value, access.NewRequirements(access.AllOf(access.SequentialWrite))),
	)
}
