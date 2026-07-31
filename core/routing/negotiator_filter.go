package routing

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/registry"
)

func (n *Negotiator) negotiateFilters(ctx context.Context, spec ConversionSpec, state *negotiationState) (resolvedSource, error) {
	aliasIndex := make(map[string]int, len(spec.Filters))
	for i, f := range spec.Filters {
		if f.Alias == "" {
			continue
		}
		if f.Alias == MainInputAlias {
			return resolvedSource{}, fmt.Errorf("filter %d alias %q is reserved", i, f.Alias)
		}
		if _, exists := spec.AuxInputs[f.Alias]; exists {
			return resolvedSource{}, fmt.Errorf("filter %d alias %q duplicates an auxiliary input name", i, f.Alias)
		}
		if _, exists := aliasIndex[f.Alias]; exists {
			return resolvedSource{}, fmt.Errorf("duplicate filter alias %q", f.Alias)
		}
		aliasIndex[f.Alias] = i
	}

	filterManifests := make([]registry.FilterManifest, len(spec.Filters))
	var err error
	for i, filterSpec := range spec.Filters {
		if filterSpec.Manifest.Factory != nil {
			filterManifests[i] = filterSpec.Manifest
			continue
		}
		filterManifests[i], err = n.filterResolver.ResolveFilter(filterSpec.Config)
		if err != nil {
			return resolvedSource{}, fmt.Errorf("resolve filter %d: %w", i, err)
		}
	}

	type portSource struct {
		port   string
		source resolvedSource
	}
	filterSources := make([][]portSource, len(spec.Filters))
	inDegree := make([]int, len(spec.Filters))
	dependents := make([][]int, len(spec.Filters))
	for i, filterSpec := range spec.Filters {
		nodeID := filterID("", i, filterSpec.Alias)
		ports := requiredPorts(filterManifests[i].TransformManifest)
		filterSources[i] = make([]portSource, 0, len(ports))
		for _, port := range ports {
			var source resolvedSource
			if ref, explicit := filterSpec.Inputs[port]; explicit {
				source, err = resolveGraphSource(ref, aliasIndex, spec.AuxInputs)
				if err != nil {
					return resolvedSource{}, fmt.Errorf("filter %d (%s) port %q: %w", i, nodeID, port, err)
				}
			} else if port == "in" {
				source = defaultBackboneSource(i, spec.Filters)
			} else {
				return resolvedSource{}, fmt.Errorf("filter %d (%s) input port %q requires a wire", i, nodeID, port)
			}
			filterSources[i] = append(filterSources[i], portSource{port: port, source: source})
			if source.filterIndex >= 0 {
				inDegree[i]++
				dependents[source.filterIndex] = append(dependents[source.filterIndex], i)
			}
		}
	}
	order, err := topologicalOrder(inDegree, dependents)
	if err != nil {
		return resolvedSource{}, err
	}

	usedSources := make(map[string]bool)
	for _, i := range order {
		filterSpec := spec.Filters[i]
		fm := filterManifests[i]
		nodeID := filterID("", i, filterSpec.Alias)
		inputSet := make(media.StreamSet, len(filterSources[i]))
		for _, ps := range filterSources[i] {
			port, source := ps.port, ps.source
			key := source.nodeID + "\x00" + source.port
			if usedSources[key] {
				return resolvedSource{}, fmt.Errorf("filter %d (%s) port %q: source %s.%s is already wired elsewhere", i, nodeID, port, source.nodeID, source.port)
			}
			usedSources[key] = true
			sourceStream, ok := state.resolvedOutputs[source.nodeID][source.port]
			if !ok {
				return resolvedSource{}, fmt.Errorf("filter %d (%s) port %q: source %q has no output port %q", i, nodeID, port, source.nodeID, source.port)
			}
			requirements, err := fm.RequirementsFor(port, inputSet, sourceStream.Codec, filterSpec.Config)
			if err != nil {
				return resolvedSource{}, fmt.Errorf("resolve filter %d (%s) port %q requirements: %w", i, nodeID, port, err)
			}
			final, err := n.resolveEdge(source, requirements, nodeID, port, sourceStream, &state.bridgeID, &state.ownedNodes, &state.allPlans, &state.graphEdges)
			if err != nil {
				return resolvedSource{}, fmt.Errorf("satisfy filter %d (%s) port %q: %w", i, nodeID, port, err)
			}
			inputSet[port] = final
		}
		filterNode, outputSet, err := fm.Factory(inputSet, registry.TransformFactoryOptions{Config: filterSpec.Config})
		if err != nil {
			return resolvedSource{}, fmt.Errorf("resolve filter %d (%s) output streams: %w", i, nodeID, err)
		}
		if err := fm.ValidateOutputs(outputSet); err != nil {
			return resolvedSource{}, errors.Join(
				fmt.Errorf("resolve filter %d (%s) output streams: %w", i, nodeID, err),
				filterNode.Close(),
			)
		}
		state.ownedNodes = append(state.ownedNodes, filterNode)
		state.resolvedOutputs[nodeID] = outputSet
		state.allPlans = append(state.allPlans, transformPlan{
			id: nodeID, role: manifest.RoleFilter, plugin: fm.Name, config: filterSpec.Config, resources: fm.Resources,
			inputs: inputSet, outputs: outputSet, node: filterNode,
		})
	}

	var sink resolvedSource
	if spec.Sink != nil {
		sink, err = resolveGraphSource(*spec.Sink, aliasIndex, spec.AuxInputs)
		if err != nil {
			return resolvedSource{}, fmt.Errorf("sink: %w", err)
		}
	} else if len(spec.Filters) > 0 {
		sink = defaultBackboneSource(len(spec.Filters), spec.Filters)
	} else {
		sink = resolvedSource{nodeID: "decoder", port: "out", filterIndex: -1}
	}
	_, ok := state.resolvedOutputs[sink.nodeID][sink.port]
	if !ok {
		return resolvedSource{}, fmt.Errorf("output: source %q has no output port %q; set Sink explicitly", sink.nodeID, sink.port)
	}

	return sink, nil
}
