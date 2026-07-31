package routing

import (
	"context"
	"fmt"

	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/registry"
)

func (n *Negotiator) negotiateEncoder(ctx context.Context, spec ConversionSpec, sink resolvedSource, state *negotiationState) (media.StreamInfo, error) {
	sinkStream := state.resolvedOutputs[sink.nodeID][sink.port]

	encoderManifest := spec.EncoderManifest
	var err error
	if encoderManifest.Name == "" {
		encoderManifest, err = n.encoderResolver.ResolveEncoder(spec.TargetCodec)
		if err != nil {
			return media.StreamInfo{}, fmt.Errorf("resolve encoder: %w", err)
		}
	} else if !encoderManifest.Supports(spec.TargetCodec) {
		return media.StreamInfo{}, fmt.Errorf("encoder %q does not support codec %q", encoderManifest.Name, spec.TargetCodec)
	}
	encodeConfig, err := configurationFor(encoderManifest, spec.EncodeConfig)
	if err != nil {
		return media.StreamInfo{}, fmt.Errorf("configure encoder %s: %w", encoderManifest.Name, err)
	}
	requirements, err := encoderManifest.Requirements("in", spec.TargetCodec, encodeConfig)
	if err != nil {
		return media.StreamInfo{}, fmt.Errorf("resolve encoder %s requirements: %w", encoderManifest.Name, err)
	}
	encoderInput, err := n.resolveEdge(sink, requirements, "encoder", "in", sinkStream, &state.bridgeID, &state.ownedNodes, &state.allPlans, &state.graphEdges)
	if err != nil {
		return media.StreamInfo{}, fmt.Errorf("satisfy encoder %s: %w", encoderManifest.Name, err)
	}
	encoderNode, encoderOutput, err := encoderManifest.Factory(encoderInput, spec.TargetCodec, registry.TransformFactoryOptions{Config: encodeConfig})
	if err != nil {
		return media.StreamInfo{}, fmt.Errorf("resolve encoder output stream: %w", err)
	}
	state.ownedNodes = append(state.ownedNodes, encoderNode)
	encoderOutput.Codec = spec.TargetCodec
	state.allPlans = append(state.allPlans, transformPlan{
		id: "encoder", role: manifest.RoleEncoder, plugin: encoderManifest.Name, config: encodeConfig, resources: encoderManifest.Resources,
		inputs: media.StreamSet{"in": encoderInput}, outputs: media.StreamSet{"out": encoderOutput}, node: encoderNode,
	})

	return encoderOutput, nil
}
