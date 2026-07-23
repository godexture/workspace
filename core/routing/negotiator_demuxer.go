package routing

import (
	"context"
	"fmt"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/core/pipeline"
)

func (n *Negotiator) negotiateDemuxer(ctx context.Context, spec ConversionSpec, state *negotiationState) (media.StreamInfo, *metadata.Bundle, error) {
	demuxManifest := spec.DemuxManifest
	var err error
	if demuxManifest.Name == "" {
		demuxManifest, err = n.demuxerResolver.ResolveDemuxer(spec.Input)
		if err != nil {
			return media.StreamInfo{}, nil, fmt.Errorf("resolve demuxer: %w", err)
		}
	}
	demuxConfig, err := configurationFor(demuxManifest, spec.DemuxConfig)
	if err != nil {
		return media.StreamInfo{}, nil, fmt.Errorf("configure demuxer %s: %w", demuxManifest.Name, err)
	}
	demuxNode, err := demuxManifest.Factory(spec.Input, demuxConfig)
	if err != nil {
		return media.StreamInfo{}, nil, fmt.Errorf("create demuxer: %w", err)
	}

	if err := state.geometry.AddNodeDef(pipeline.NodeDef{
		ID:   "demuxer",
		Node: demuxNode,
		Description: pipeline.NodeDescription{
			Role: manifest.RoleDemuxer, Plugin: demuxManifest.Name, Configuration: demuxConfig,
		},
	}); err != nil {
		demuxNode.Close()
		return media.StreamInfo{}, nil, err
	}

	streams, err := demuxNode.Streams()
	if err != nil {
		return media.StreamInfo{}, nil, fmt.Errorf("get input streams: %w", err)
	}
	if len(streams) == 0 {
		return media.StreamInfo{}, nil, fmt.Errorf("no streams found in input")
	}
	if err := state.geometry.SetNodeDescription("demuxer", pipeline.NodeDescription{
		Role: manifest.RoleDemuxer, Plugin: demuxManifest.Name, Configuration: demuxConfig, Outputs: streams,
	}); err != nil {
		return media.StreamInfo{}, nil, err
	}

	inputStream := streams[0]
	if spec.SelectInputStream != nil {
		inputStream, err = spec.SelectInputStream(streams)
		if err != nil {
			return media.StreamInfo{}, nil, fmt.Errorf("select input stream: %w", err)
		}
	}

	var meta *metadata.Bundle
	if demuxNode.Metadata() != nil {
		meta = demuxNode.Metadata().Clone()
	}

	return inputStream, meta, nil
}
