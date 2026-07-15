package routing

import (
	"context"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/core/registry"
	"github.com/godexture/core/resolver"
)

type Negotiator struct {
	demuxResolver resolver.DemuxerResolver
	decResolver   resolver.DecoderResolver
	encResolver   resolver.EncoderResolver
	muxResolver   resolver.MuxerResolver
}

func NewNegotiator(
	mux resolver.MuxerResolver,
	demux resolver.DemuxerResolver,
	enc resolver.EncoderResolver,
	dec resolver.DecoderResolver,
) *Negotiator {
	return &Negotiator{
		demuxResolver: demux,
		decResolver:   dec,
		encResolver:   enc,
		muxResolver:   mux,
	}
}

type ConversionSpec struct {
	Input  io.ReadSeeker
	Output io.Writer

	DemuxConfig       registry.Configuration
	SelectInputStream func(streams []media.StreamInfo) (media.StreamInfo, error)

	DecodeConfig registry.Configuration

	TargetCodec  media.CodecID
	EncodeConfig registry.Configuration

	MuxConfig           registry.Configuration
	PrepareOutputStream func(inStream media.StreamInfo) media.StreamInfo
}

func (n *Negotiator) NegotiateConversion(ctx context.Context, spec ConversionSpec) (*pipeline.Geometry, error) {
	if n.decResolver == nil || n.encResolver == nil || n.demuxResolver == nil || n.muxResolver == nil {
		return nil, fmt.Errorf("all resolvers must be provided")
	}

	if spec.Input == nil {
		return nil, fmt.Errorf("input must not be nil")
	}

	if spec.Output == nil {
		return nil, fmt.Errorf("output must not be nil")
	}

	// 1. Resolve & create Demuxer
	demuxManifest, err := n.demuxResolver.ResolveDemuxer(spec.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve demuxer: %w", err)
	}
	demuxNode, err := demuxManifest.Factory(spec.Input, spec.DemuxConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create demuxer: %w", err)
	}

	streams, err := demuxNode.Streams()
	if err != nil {
		return nil, fmt.Errorf("failed to get streams: %w", err)
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("no streams found in input")
	}

	inputStream := streams[0]
	if spec.SelectInputStream != nil {
		inputStream, err = spec.SelectInputStream(streams)
		if err != nil {
			return nil, fmt.Errorf("failed to select input stream: %w", err)
		}
	}

	// 2. Resolve & create Decoder
	decManifest, err := n.decResolver.ResolveDecoder(inputStream)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve decoder: %w", err)
	}

	decNode, err := decManifest.Factory(inputStream, spec.DecodeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create decoder: %w", err)
	}

	// Prepare intermediate stream (uncompressed audio/video properties)
	intermediateStream := inputStream
	if decManifest.TransformFunc != nil {
		decProfile, err := decManifest.Transform(intermediateStream, inputStream.Codec, spec.DecodeConfig)
		if err != nil {
			return nil, fmt.Errorf("resolve decoder output stream: %w", err)
		}
		intermediateStream.Type = decProfile.Type
		intermediateStream.MediaAttributes = decProfile.MediaAttributes
	}

	// 3. Resolve & create Encoder
	encManifest, err := n.encResolver.ResolveEncoder(spec.TargetCodec)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve encoder: %w", err)
	}
	encNode, err := encManifest.Factory(intermediateStream, spec.TargetCodec, spec.EncodeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}

	// 4. Resolve & create Muxer
	muxManifest, err := n.muxResolver.ResolveMuxer(spec.MuxConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve muxer: %w", err)
	}
	muxNode, err := muxManifest.Factory(spec.Output, spec.MuxConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create muxer: %w", err)
	}

	// 5. Prepare output stream and add to muxer
	outputStream := intermediateStream

	// Resolve the output profile with the same target codec and configuration
	// used to construct the encoder.
	if encManifest.TransformFunc != nil {
		encProfile, err := encManifest.Transform(outputStream, spec.TargetCodec, spec.EncodeConfig)
		if err != nil {
			return nil, fmt.Errorf("resolve encoder output stream: %w", err)
		}
		outputStream.Type = encProfile.Type
		outputStream.MediaAttributes = encProfile.MediaAttributes
	}

	// Fallback/override to ensure TargetCodec is explicitly set on the output stream
	outputStream.Codec = spec.TargetCodec

	if spec.PrepareOutputStream != nil {
		outputStream = spec.PrepareOutputStream(outputStream)
	}
	if _, err := muxNode.AddStream(outputStream); err != nil {
		return nil, fmt.Errorf("failed to add stream to muxer: %w", err)
	}

	// 6. Build Geometry
	geo := pipeline.NewGeometry()
	geo.AddNode("demuxer", demuxNode)
	geo.AddNode("decoder", decNode)
	geo.AddNode("encoder", encNode)
	geo.AddNode("muxer", muxNode)

	geo.AddEdge("demuxer", "out", "decoder", "in")
	geo.AddEdge("decoder", "out", "encoder", "in")
	geo.AddEdge("encoder", "out", "muxer", "in")

	return geo, nil
}
