package catalog

import (
	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/bound"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type accessScheme struct {
	direction string
	scheme    string
}

func validateTraits(components []plugin.Component) []diagnostic.Item {
	seen := make(map[accessScheme]plugin.Identity)
	var items []diagnostic.Item
	for _, component := range components {
		shape := component.Ports()
		if trait, ok := access.SourceOf(component); ok {
			items = append(items, validateSourceTrait(component, shape, trait, seen)...)
		}
		if trait, ok := access.SinkOf(component); ok {
			items = append(items, validateSinkTrait(component, shape, trait, seen)...)
		}
		if trait, ok := mediaformat.ReadOf(component); ok {
			items = append(items, validateFormatReadTrait(component, shape, trait)...)
		}
		if trait, ok := mediaformat.WriteOf(component); ok {
			items = append(items, validateFormatWriteTrait(component, shape, trait)...)
		}
		if trait, ok := endpoint.TraitOf(component); ok {
			if !trait.Valid() {
				items = append(items, traitItem("catalog.endpoint-trait", component.Identity(), "Endpoint trait is invalid", nil))
			}
			_, source := bound.Port(shape, plan.InputBoundary)
			_, sink := bound.Port(shape, plan.OutputBoundary)
			if !source && !sink {
				items = append(items, traitItem("catalog.endpoint-shape", component.Identity(), "Endpoint trait requires one directional port", nil))
			}
		}
	}
	return items
}

func validateSourceTrait(component plugin.Component, shape flow.Shape, trait access.SourceTrait, seen map[accessScheme]plugin.Identity) []diagnostic.Item {
	var items []diagnostic.Item
	if !trait.Valid() {
		items = append(items, traitItem("catalog.access-trait", component.Identity(), "Access source trait is invalid", map[string]string{"direction": "source", "scheme": trait.Scheme()}))
	}
	if port, ok := bound.Port(shape, plan.InputBoundary); !ok {
		items = append(items, traitItem("catalog.access-shape", component.Identity(), "Access source trait requires a 0-input/1-output component", map[string]string{"direction": "source", "scheme": trait.Scheme()}))
	} else if !canonicalBytes(port) {
		items = append(items, traitItem("catalog.access-schema", component.Identity(), "Access source trait output must use access.Bytes", map[string]string{"direction": "source", "port": port.ID(), "scheme": trait.Scheme()}))
	}
	return append(items, validateScheme(component.Identity(), "source", trait.Scheme(), trait.Valid(), seen)...)
}

func validateSinkTrait(component plugin.Component, shape flow.Shape, trait access.SinkTrait, seen map[accessScheme]plugin.Identity) []diagnostic.Item {
	var items []diagnostic.Item
	if !trait.Valid() {
		items = append(items, traitItem("catalog.access-trait", component.Identity(), "Access sink trait is invalid", map[string]string{"direction": "sink", "scheme": trait.Scheme()}))
	}
	if port, ok := bound.Port(shape, plan.OutputBoundary); !ok {
		items = append(items, traitItem("catalog.access-shape", component.Identity(), "Access sink trait requires a 1-input/0-output component", map[string]string{"direction": "sink", "scheme": trait.Scheme()}))
	} else if !canonicalWrites(port) {
		items = append(items, traitItem("catalog.access-schema", component.Identity(), "Access sink trait input must use access.Writes", map[string]string{"direction": "sink", "port": port.ID(), "scheme": trait.Scheme()}))
	}
	return append(items, validateScheme(component.Identity(), "sink", trait.Scheme(), trait.Valid(), seen)...)
}

func validateFormatReadTrait(component plugin.Component, shape flow.Shape, trait mediaformat.ReadTrait) []diagnostic.Item {
	var items []diagnostic.Item
	if !trait.Valid() {
		items = append(items, traitItem("catalog.format-trait", component.Identity(), "Format read trait is invalid", map[string]string{"direction": "read", "format": trait.Format().Identity().String()}))
	}
	if len(shape.Inputs) != 1 || shape.Inputs[0].Multiplicity() != flow.One {
		items = append(items, traitItem("catalog.format-shape", component.Identity(), "Format read trait requires exactly one input port", map[string]string{"direction": "read"}))
	} else if !canonicalBytes(shape.Inputs[0]) {
		items = append(items, traitItem("catalog.format-schema", component.Identity(), "Format read trait input must use access.Bytes", map[string]string{"direction": "read", "port": shape.Inputs[0].ID()}))
	}
	return items
}

func validateFormatWriteTrait(component plugin.Component, shape flow.Shape, trait mediaformat.WriteTrait) []diagnostic.Item {
	var items []diagnostic.Item
	if !trait.Valid() {
		items = append(items, traitItem("catalog.format-trait", component.Identity(), "Format write trait is invalid", map[string]string{"direction": "write", "format": trait.Format().Identity().String()}))
	}
	if len(shape.Outputs) != 1 || shape.Outputs[0].Multiplicity() != flow.One {
		items = append(items, traitItem("catalog.format-shape", component.Identity(), "Format write trait requires exactly one output port", map[string]string{"direction": "write"}))
	} else if !canonicalWrites(shape.Outputs[0]) {
		items = append(items, traitItem("catalog.format-schema", component.Identity(), "Format write trait output must use access.Writes", map[string]string{"direction": "write", "port": shape.Outputs[0].ID()}))
	}
	return items
}

func canonicalBytes(port flow.Port) bool {
	want := access.Bytes().Descriptor()
	got := port.Schema()
	return got.Identity() == want.Identity() && got.Payload() == want.Payload()
}

func canonicalWrites(port flow.Port) bool {
	want := access.Writes().Descriptor()
	got := port.Schema()
	return got.Identity() == want.Identity() && got.Payload() == want.Payload()
}

func validateScheme(identity plugin.Identity, direction, scheme string, valid bool, seen map[accessScheme]plugin.Identity) []diagnostic.Item {
	if !valid {
		return nil
	}
	key := accessScheme{direction: direction, scheme: scheme}
	previous, exists := seen[key]
	if !exists {
		seen[key] = identity
		return nil
	}
	return []diagnostic.Item{traitItem("catalog.access-scheme", identity, "Access scheme is repeated for one direction", map[string]string{
		"direction": direction,
		"scheme":    scheme,
		"previous":  previous.String(),
	})}
}

func traitItem(code string, component plugin.Identity, message string, detail map[string]string) diagnostic.Item {
	return diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.Path{Component: component.String()}, message, detail)
}
