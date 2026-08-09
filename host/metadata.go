package host

import (
	"fmt"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/carrier"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/plugin"
)

func (h *Host) formatCompileContexts(requested job.Graph) (map[job.NodeID]plugin.CompileContext, error) {
	contexts := make(map[job.NodeID]plugin.CompileContext)
	for _, node := range requested.Nodes() {
		component, ok := h.index.Lookup(node.Component())
		if !ok {
			continue
		}
		carriers := formatCarriers(component)
		if len(carriers) == 0 {
			continue
		}
		resolver, err := h.metadataResolver(carriers)
		if err != nil {
			return nil, diagnostic.NewError(diagnostic.NewItem(
				"prepare.metadata-resolver",
				diagnostic.ErrorSeverity,
				diagnostic.Path{Component: component.Identity().String()},
				"Format metadata bindings could not be resolved",
				map[string]string{"node": node.ID().String(), "cause": err.Error()},
			))
		}
		prepared, err := metadata.WithResolver(plugin.CompileContext{}, resolver)
		if err != nil {
			return nil, err
		}
		contexts[node.ID()] = prepared
	}
	return contexts, nil
}

func (h *Host) metadataResolver(carriers []carrier.ID) (metadata.Resolver, error) {
	components := make(map[carrier.ID]plugin.Component, len(carriers))
	for _, slot := range carriers {
		declaration, ok := h.index.LookupDeclaration(metadata.BindingKey(slot))
		if !ok {
			continue
		}
		targets := declaration.Targets()
		if len(targets) != 1 {
			return metadata.Resolver{}, fmt.Errorf("metadata binding %s has %d targets", declaration.Key(), len(targets))
		}
		identity, ok := targets[0].Component()
		if !ok {
			return metadata.Resolver{}, fmt.Errorf("metadata binding %s target is not a component", declaration.Key())
		}
		component, ok := h.index.Lookup(identity)
		if !ok {
			return metadata.Resolver{}, fmt.Errorf("metadata binding %s target %s is absent", declaration.Key(), identity)
		}
		components[slot] = component
	}
	return metadata.NewResolver(components)
}

func formatCarriers(component plugin.Component) []carrier.ID {
	seen := make(map[carrier.ID]struct{})
	var result []carrier.ID
	appendFormat := func(value mediaformat.Format) {
		for _, slot := range value.Carriers() {
			if _, ok := seen[slot]; ok {
				continue
			}
			seen[slot] = struct{}{}
			result = append(result, slot)
		}
	}
	if trait, ok := mediaformat.ReadOf(component); ok && trait.Valid() {
		appendFormat(trait.Format())
	}
	if trait, ok := mediaformat.WriteOf(component); ok && trait.Valid() {
		appendFormat(trait.Format())
	}
	return result
}
