package routing

import (
	"context"
	"fmt"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/core/pipeline"
)

func (n *Negotiator) negotiateMuxer(ctx context.Context, spec ConversionSpec, encoderOutput media.StreamInfo, demuxMetadata *metadata.Bundle, state *negotiationState) error {
	muxManifest := spec.MuxManifest
	var err error
	if muxManifest.Name == "" {
		muxManifest, err = n.muxerResolver.ResolveMuxer(spec.MuxConfig)
		if err != nil {
			return fmt.Errorf("resolve muxer: %w", err)
		}
	}
	if !muxManifest.Supports(spec.TargetCodec) {
		return fmt.Errorf("muxer %q does not support codec %q", muxManifest.Name, spec.TargetCodec)
	}
	muxConfig, err := configurationFor(muxManifest, spec.MuxConfig)
	if err != nil {
		return fmt.Errorf("configure muxer %s: %w", muxManifest.Name, err)
	}

	muxNode, err := muxManifest.Factory(spec.Output, muxConfig)
	if err != nil {
		return fmt.Errorf("create muxer: %w", err)
	}

	state.ownedNodes = append(state.ownedNodes, muxNode)
	if err := state.geometry.AddNodeDef(pipeline.NodeDef{
		ID:   "muxer",
		Node: muxNode,
		Description: pipeline.NodeDescription{
			Role: manifest.RoleMuxer, Plugin: muxManifest.Name, Configuration: muxConfig,
		},
	}); err != nil {
		return fmt.Errorf("add muxer to geometry: %w", err)
	}
	state.ownedNodes = releaseOwnedNode(state.ownedNodes, muxNode)
	if err := muxNode.SetMetadata(demuxMetadata); err != nil {
		return fmt.Errorf("set muxer metadata: %w", err)
	}

	outputStream := encoderOutput
	if spec.PrepareOutputStream != nil {
		outputStream = spec.PrepareOutputStream(outputStream)
	}
	outputIndex, err := muxNode.AddStream(outputStream)
	if err != nil {
		return fmt.Errorf("add output stream to muxer: %w", err)
	}
	outputStream.Index = outputIndex
	if err := state.geometry.SetNodeDescription("muxer", pipeline.NodeDescription{
		Role: manifest.RoleMuxer, Plugin: muxManifest.Name, Configuration: muxConfig, Inputs: []media.StreamInfo{outputStream},
	}); err != nil {
		return err
	}
	state.graphEdges = append(state.graphEdges, pipeline.EdgeDef{FromNode: "encoder", FromPort: "out", ToNode: "muxer", ToPort: "in", Stream: outputStream})

	return nil
}
