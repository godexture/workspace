package routing

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/pipeline"
)

func (n *Negotiator) validatePlaybackSpec(ctx context.Context, spec PlaybackSpec) error {
	if ctx == nil {
		return fmt.Errorf("context must not be nil")
	}
	if n.decoderResolver == nil || n.demuxerResolver == nil {
		return fmt.Errorf("demuxer and decoder resolvers must be provided")
	}
	needsFilterResolver := false
	for _, filterSpec := range spec.Filters {
		if filterSpec.Manifest.Factory == nil {
			needsFilterResolver = true
			break
		}
	}
	if needsFilterResolver && n.filterResolver == nil {
		return fmt.Errorf("filter resolver must be provided when filters are requested")
	}
	if spec.Input == nil {
		return fmt.Errorf("input must not be nil")
	}
	if len(spec.SinkRequirements) == 0 {
		return fmt.Errorf("sink requirements must not be empty")
	}
	if spec.SinkFactory == nil {
		return fmt.Errorf("sink factory must not be nil")
	}
	if spec.Resources.Parallelism < 0 {
		return fmt.Errorf("parallelism budget must not be negative: %d", spec.Resources.Parallelism)
	}
	return ctx.Err()
}

func (n *Negotiator) NegotiatePlayback(ctx context.Context, spec PlaybackSpec) (result *pipeline.Geometry, resultErr error) {
	if err := n.validatePlaybackSpec(ctx, spec); err != nil {
		return nil, err
	}

	state := &negotiationState{
		geometry:        pipeline.NewGeometry(),
		ownedNodes:      make([]node.Node, 0),
		resolvedOutputs: make(map[string]media.StreamSet),
	}
	defer func() {
		if result == nil {
			resultErr = errors.Join(resultErr, closeOwnedNodes(state.ownedNodes))
			resultErr = errors.Join(resultErr, state.geometry.Close())
		}
	}()

	inputStream, _, err := n.negotiateDemuxer(ctx, ConversionSpec{
		Input: spec.Input, DemuxManifest: spec.DemuxManifest, DemuxConfig: spec.DemuxConfig, SelectInputStream: spec.SelectInputStream,
	}, state)
	if err != nil {
		return nil, err
	}
	auxSources, auxPlans, err := n.negotiateAuxSources(ctx, spec.AuxInputs, state.geometry, &state.ownedNodes, &state.graphEdges)
	if err != nil {
		return nil, err
	}
	state.allPlans = append(state.allPlans, auxPlans...)
	for _, source := range auxSources {
		state.resolvedOutputs[source.decoderID] = media.StreamSet{"out": source.output}
	}
	if err := n.negotiateDecoder(ctx, ConversionSpec{
		DecoderManifest: spec.DecoderManifest, DecodeConfig: spec.DecodeConfig,
	}, inputStream, state); err != nil {
		return nil, err
	}
	source, err := n.negotiateFilters(ctx, ConversionSpec{
		Filters: spec.Filters, AuxInputs: spec.AuxInputs, Sink: spec.Sink,
	}, state)
	if err != nil {
		return nil, err
	}
	sourceStream := state.resolvedOutputs[source.nodeID][source.port]
	resolvedStream, err := n.resolveEdge(source, spec.SinkRequirements, "sink", "in", sourceStream, &state.bridgeID, &state.ownedNodes, &state.allPlans, &state.graphEdges)
	if err != nil {
		return nil, fmt.Errorf("satisfy sink: %w", err)
	}
	sink, err := spec.SinkFactory(resolvedStream)
	if err != nil {
		return nil, fmt.Errorf("create sink: %w", err)
	}
	state.ownedNodes = append(state.ownedNodes, sink)
	if err := n.addNodesToGeometry(spec.Resources, state); err != nil {
		return nil, err
	}
	name := spec.SinkName
	if name == "" {
		name = "sink"
	}
	if err := state.geometry.AddNodeDef(pipeline.NodeDef{
		ID: "sink", Node: sink,
		Description: pipeline.NodeDescription{Role: manifest.RoleSink, Plugin: name, Inputs: []media.StreamInfo{resolvedStream}},
	}); err != nil {
		return nil, err
	}
	state.ownedNodes = releaseOwnedNode(state.ownedNodes, sink)
	for _, edge := range state.graphEdges {
		if err := state.geometry.AddEdgeDef(edge); err != nil {
			return nil, err
		}
	}
	return state.geometry, nil
}
