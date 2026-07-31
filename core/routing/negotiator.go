package routing

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/core/resolver"
)

type Negotiator struct {
	demuxerResolver resolver.DemuxerResolver
	decoderResolver resolver.DecoderResolver
	encoderResolver resolver.EncoderResolver
	muxerResolver   resolver.MuxerResolver
	filterResolver  resolver.FilterResolver
	bridgeResolver  resolver.BridgeResolver
}

func NewNegotiator(
	muxer resolver.MuxerResolver,
	demuxer resolver.DemuxerResolver,
	encoder resolver.EncoderResolver,
	decoder resolver.DecoderResolver,
	filter resolver.FilterResolver,
	bridge resolver.BridgeResolver,
) *Negotiator {
	return &Negotiator{
		demuxerResolver: demuxer,
		decoderResolver: decoder,
		encoderResolver: encoder,
		muxerResolver:   muxer,
		filterResolver:  filter,
		bridgeResolver:  bridge,
	}
}

type transformPlan struct {
	id           string
	role         manifest.NodeType
	plugin       string
	config       registry.Configuration
	resources    registry.ResourceRequest
	inputs       media.StreamSet
	outputs      media.StreamSet
	autoInserted bool
	node         node.Node
}

type resolvedSource struct {
	nodeID      string
	port        string
	filterIndex int // -1 when the source is not a filter (main input or auxiliary input)
}

type negotiationState struct {
	geometry        *pipeline.Geometry
	ownedNodes      []node.Node
	allPlans        []transformPlan
	graphEdges      []pipeline.EdgeDef
	resolvedOutputs map[string]media.StreamSet
	bridgeID        int
}

func (n *Negotiator) validateSpec(ctx context.Context, spec ConversionSpec) error {
	if ctx == nil {
		return fmt.Errorf("context must not be nil")
	}
	if n.decoderResolver == nil || n.encoderResolver == nil || n.demuxerResolver == nil || n.muxerResolver == nil {
		return fmt.Errorf("muxer, demuxer, encoder, and decoder resolvers must be provided")
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
	if spec.Output == nil {
		return fmt.Errorf("output must not be nil")
	}
	if spec.Resources.Parallelism < 0 {
		return fmt.Errorf("parallelism budget must not be negative: %d", spec.Resources.Parallelism)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (n *Negotiator) addNodesToGeometry(resources registry.ResourceBudget, state *negotiationState) error {
	parallelism := resources.Parallelism
	if parallelism == 0 {
		parallelism = runtime.GOMAXPROCS(0)
	}
	requests := make([]registry.ResourceRequest, len(state.allPlans))
	needsPool := false
	for i := range state.allPlans {
		requests[i] = state.allPlans[i].resources
		needsPool = needsPool || requests[i].Parallelism
	}

	var pool *registry.WorkerPool
	if needsPool && parallelism > 1 {
		pool = registry.NewWorkerPool(parallelism)
		if err := state.geometry.AddResourceCloser(pool.Close); err != nil {
			return fmt.Errorf("register resource pool: %w", err)
		}
	}
	grants := grantResources(requests, pool)

	for i, plan := range state.allPlans {
		if err := state.geometry.AddNodeDef(pipeline.NodeDef{
			ID:   plan.id,
			Node: plan.node,
			Description: pipeline.NodeDescription{
				Role:          plan.role,
				Plugin:        plan.plugin,
				Configuration: plan.config,
				Resources:     grants[i],
				Inputs:        streamValues(plan.inputs),
				Outputs:       streamValues(plan.outputs),
				AutoInserted:  plan.autoInserted,
			},
		}); err != nil {
			return fmt.Errorf("add %s to geometry: %w", plan.id, err)
		}
		state.ownedNodes = releaseOwnedNode(state.ownedNodes, plan.node)
	}
	return nil
}

func (n *Negotiator) NegotiateConversion(ctx context.Context, spec ConversionSpec) (result *pipeline.Geometry, resultErr error) {
	if err := n.validateSpec(ctx, spec); err != nil {
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

	inputStream, demuxMetadata, err := n.negotiateDemuxer(ctx, spec, state)
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

	if err := n.negotiateDecoder(ctx, spec, inputStream, state); err != nil {
		return nil, err
	}

	sink, err := n.negotiateFilters(ctx, spec, state)
	if err != nil {
		return nil, err
	}

	encoderOutput, err := n.negotiateEncoder(ctx, spec, sink, state)
	if err != nil {
		return nil, err
	}

	if err := n.addNodesToGeometry(spec.Resources, state); err != nil {
		return nil, err
	}

	if err := n.negotiateMuxer(ctx, spec, encoderOutput, demuxMetadata, state); err != nil {
		return nil, err
	}

	for _, edge := range state.graphEdges {
		if err := state.geometry.AddEdgeDef(edge); err != nil {
			return nil, err
		}
	}

	return state.geometry, nil
}
