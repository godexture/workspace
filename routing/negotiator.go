package routing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/core/registry"
	"github.com/godexture/core/resolver"
)

type Negotiator struct {
	demuxerResolver resolver.DemuxerResolver
	decoderResolver resolver.DecoderResolver
	encoderResolver resolver.EncoderResolver
	muxerResolver   resolver.MuxerResolver
	filterResolver  resolver.FilterResolver
}

func NewNegotiator(
	muxer resolver.MuxerResolver,
	demuxer resolver.DemuxerResolver,
	encoder resolver.EncoderResolver,
	decoder resolver.DecoderResolver,
	filter resolver.FilterResolver,
) *Negotiator {
	return &Negotiator{
		demuxerResolver: demuxer,
		decoderResolver: decoder,
		encoderResolver: encoder,
		muxerResolver:   muxer,
		filterResolver:  filter,
	}
}

type FilterSpec struct {
	Config registry.Configuration
}

type ConversionSpec struct {
	Input  io.ReadSeeker
	Output io.Writer

	DemuxConfig       registry.Configuration
	SelectInputStream func(streams []media.StreamInfo) (media.StreamInfo, error)

	DecodeConfig registry.Configuration
	Filters      []FilterSpec

	TargetCodec  media.CodecID
	EncodeConfig registry.Configuration

	MuxConfig           registry.Configuration
	PrepareOutputStream func(inStream media.StreamInfo) media.StreamInfo

	// Resources sets the total execution budget. Parallelism == 0 uses
	// runtime.GOMAXPROCS(0).
	Resources registry.ResourceBudget
}

type transformPlan struct {
	id        string
	config    registry.Configuration
	resources registry.ResourceRequest
	factory   func(registry.TransformFactoryOptions) (node.Node, error)
}

func (n *Negotiator) NegotiateConversion(ctx context.Context, spec ConversionSpec) (result *pipeline.Geometry, resultErr error) {
	if ctx == nil {
		return nil, fmt.Errorf("context must not be nil")
	}
	if n.decoderResolver == nil || n.encoderResolver == nil || n.demuxerResolver == nil || n.muxerResolver == nil {
		return nil, fmt.Errorf("muxer, demuxer, encoder, and decoder resolvers must be provided")
	}
	if len(spec.Filters) > 0 && n.filterResolver == nil {
		return nil, fmt.Errorf("filter resolver must be provided when filters are requested")
	}
	if spec.Input == nil {
		return nil, fmt.Errorf("input must not be nil")
	}
	if spec.Output == nil {
		return nil, fmt.Errorf("output must not be nil")
	}
	if spec.Resources.Parallelism < 0 {
		return nil, fmt.Errorf("parallelism budget must not be negative: %d", spec.Resources.Parallelism)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	demuxManifest, err := n.demuxerResolver.ResolveDemuxer(spec.Input)
	if err != nil {
		return nil, fmt.Errorf("resolve demuxer: %w", err)
	}
	demuxNode, err := demuxManifest.Factory(spec.Input, spec.DemuxConfig)
	if err != nil {
		return nil, fmt.Errorf("create demuxer: %w", err)
	}
	geometry := pipeline.NewGeometry()
	if err := geometry.AddNode("demuxer", demuxNode); err != nil {
		return nil, err
	}
	defer func() {
		if result == nil {
			resultErr = errors.Join(resultErr, geometry.Close())
		}
	}()
	streams, err := demuxNode.Streams()
	if err != nil {
		return nil, fmt.Errorf("get input streams: %w", err)
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("no streams found in input")
	}

	inputStream := streams[0]
	if spec.SelectInputStream != nil {
		inputStream, err = spec.SelectInputStream(streams)
		if err != nil {
			return nil, fmt.Errorf("select input stream: %w", err)
		}
	}

	plans := make([]transformPlan, 0, 2+len(spec.Filters))
	currentStream := inputStream

	decoderManifest, err := n.decoderResolver.ResolveDecoder(currentStream)
	if err != nil {
		return nil, fmt.Errorf("resolve decoder: %w", err)
	}
	decoderOutput, err := transformStream(decoderManifest.TransformManifest, currentStream, currentStream.Codec, spec.DecodeConfig)
	if err != nil {
		return nil, fmt.Errorf("resolve decoder output stream: %w", err)
	}
	decoderInput := currentStream
	plans = append(plans, transformPlan{
		id:        "decoder",
		config:    spec.DecodeConfig,
		resources: decoderManifest.Resources,
		factory: func(options registry.TransformFactoryOptions) (node.Node, error) {
			return decoderManifest.Factory(decoderInput, options)
		},
	})
	currentStream = decoderOutput

	for i, filterSpec := range spec.Filters {
		filterManifest, err := n.filterResolver.ResolveFilter(filterSpec.Config)
		if err != nil {
			return nil, fmt.Errorf("resolve filter %d: %w", i, err)
		}
		if !filterManifest.Accept(currentStream) {
			return nil, fmt.Errorf("filter %d (%s) does not accept stream %s", i, filterManifest.Name, currentStream.Codec)
		}
		filterOutput, err := transformStream(filterManifest.TransformManifest, currentStream, currentStream.Codec, filterSpec.Config)
		if err != nil {
			return nil, fmt.Errorf("resolve filter %d output stream: %w", i, err)
		}
		filterInput := currentStream
		manifest := filterManifest
		plans = append(plans, transformPlan{
			id:        fmt.Sprintf("filter:%d", i),
			config:    filterSpec.Config,
			resources: manifest.Resources,
			factory: func(options registry.TransformFactoryOptions) (node.Node, error) {
				return manifest.Factory(filterInput, options)
			},
		})
		currentStream = filterOutput
	}

	encoderManifest, err := n.encoderResolver.ResolveEncoder(spec.TargetCodec)
	if err != nil {
		return nil, fmt.Errorf("resolve encoder: %w", err)
	}
	if !encoderManifest.Accept(currentStream) {
		return nil, fmt.Errorf("encoder %s does not accept stream %s", encoderManifest.Name, currentStream.Codec)
	}
	encoderOutput, err := transformStream(encoderManifest.TransformManifest, currentStream, spec.TargetCodec, spec.EncodeConfig)
	if err != nil {
		return nil, fmt.Errorf("resolve encoder output stream: %w", err)
	}
	encoderOutput.Codec = spec.TargetCodec
	encoderInput := currentStream
	plans = append(plans, transformPlan{
		id:        "encoder",
		config:    spec.EncodeConfig,
		resources: encoderManifest.Resources,
		factory: func(options registry.TransformFactoryOptions) (node.Node, error) {
			return encoderManifest.Factory(encoderInput, spec.TargetCodec, options)
		},
	})

	muxManifest, err := n.muxerResolver.ResolveMuxer(spec.MuxConfig)
	if err != nil {
		return nil, fmt.Errorf("resolve muxer: %w", err)
	}

	parallelism := spec.Resources.Parallelism
	if parallelism == 0 {
		parallelism = runtime.GOMAXPROCS(0)
	}
	requests := make([]registry.ResourceRequest, len(plans))
	for i := range plans {
		requests[i] = plans[i].resources
	}
	allocations := allocateResources(requests, parallelism)

	transformNodes := make([]node.Node, len(plans))
	for i, plan := range plans {
		transformNodes[i], err = plan.factory(registry.TransformFactoryOptions{
			Config:    plan.config,
			Resources: allocations[i],
		})
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", plan.id, err)
		}
		if err := geometry.AddNode(plan.id, transformNodes[i]); err != nil {
			return nil, fmt.Errorf("add %s to geometry: %w", plan.id, err)
		}
	}

	muxNode, err := muxManifest.Factory(spec.Output, spec.MuxConfig)
	if err != nil {
		return nil, fmt.Errorf("create muxer: %w", err)
	}
	if err := geometry.AddNode("muxer", muxNode); err != nil {
		return nil, fmt.Errorf("add muxer to geometry: %w", err)
	}

	outputStream := encoderOutput
	if spec.PrepareOutputStream != nil {
		outputStream = spec.PrepareOutputStream(outputStream)
	}
	if _, err := muxNode.AddStream(outputStream); err != nil {
		return nil, fmt.Errorf("add output stream to muxer: %w", err)
	}

	previous := "demuxer"
	previousPort := "out"
	for _, plan := range plans {
		if err := geometry.AddEdge(previous, previousPort, plan.id, "in"); err != nil {
			return nil, err
		}
		previous = plan.id
		previousPort = "out"
	}
	if err := geometry.AddEdge(previous, previousPort, "muxer", "in"); err != nil {
		return nil, err
	}
	return geometry, nil
}

func transformStream(
	transform registry.TransformManifest,
	stream media.StreamInfo,
	target media.CodecID,
	config registry.Configuration,
) (media.StreamInfo, error) {
	if transform.TransformFunc == nil {
		return stream, nil
	}
	profile, err := transform.Transform(stream, target, config)
	if err != nil {
		return media.StreamInfo{}, err
	}
	stream.Type = profile.Type
	stream.MediaAttributes = profile.MediaAttributes
	return stream, nil
}
