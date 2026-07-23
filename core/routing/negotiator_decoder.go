package routing

import (
	"context"
	"fmt"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/core/registry"
)

func (n *Negotiator) negotiateDecoder(ctx context.Context, spec ConversionSpec, inputStream media.StreamInfo, state *negotiationState) error {
	decoderManifest := spec.DecoderManifest
	var err error
	if decoderManifest.Name == "" {
		decoderManifest, err = n.decoderResolver.ResolveDecoder(inputStream)
		if err != nil {
			return fmt.Errorf("resolve decoder: %w", err)
		}
	}
	decodeConfig, err := configurationFor(decoderManifest, spec.DecodeConfig)
	if err != nil {
		return fmt.Errorf("configure decoder %s: %w", decoderManifest.Name, err)
	}
	if spec.DecoderManifest.Name != "" {
		accepted, err := decoderManifest.Accept("in", inputStream, inputStream.Codec, decodeConfig)
		if err != nil {
			return fmt.Errorf("check decoder %s: %w", decoderManifest.Name, err)
		}
		if !accepted {
			return fmt.Errorf("decoder %q does not accept input codec %q", decoderManifest.Name, inputStream.Codec)
		}
	}
	decoderNode, decoderOutput, err := decoderManifest.Factory(inputStream, registry.TransformFactoryOptions{Config: decodeConfig})
	if err != nil {
		return fmt.Errorf("resolve decoder output stream: %w", err)
	}
	state.ownedNodes = append(state.ownedNodes, decoderNode)
	state.allPlans = append(state.allPlans, transformPlan{
		id: "decoder", role: manifest.RoleDecoder, plugin: decoderManifest.Name, config: decodeConfig, resources: decoderManifest.Resources,
		inputs: media.StreamSet{"in": inputStream}, outputs: media.StreamSet{"out": decoderOutput}, node: decoderNode,
	})
	state.graphEdges = append(state.graphEdges, pipeline.EdgeDef{FromNode: "demuxer", FromPort: "out", ToNode: "decoder", ToPort: "in", Stream: inputStream, ProgressSource: true})

	state.resolvedOutputs["decoder"] = media.StreamSet{"out": decoderOutput}
	return nil
}
