package routing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"

	"github.com/godexture/core/domain/manifest"
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

type FilterSpec struct {
	Config registry.Configuration
}

type ConversionSpec struct {
	Input  io.ReadSeeker
	Output io.Writer

	DemuxManifest     registry.DemuxerManifest
	DemuxConfig       registry.Configuration
	SelectInputStream func(streams []media.StreamInfo) (media.StreamInfo, error)

	DecoderManifest registry.DecoderManifest
	DecodeConfig    registry.Configuration
	Filters         []FilterSpec

	EncoderManifest registry.EncoderManifest
	TargetCodec     media.CodecID
	EncodeConfig    registry.Configuration

	MuxManifest         registry.MuxerManifest
	MuxConfig           registry.Configuration
	PrepareOutputStream func(inStream media.StreamInfo) media.StreamInfo

	// Resources sets the total execution budget. Parallelism == 0 uses
	// runtime.GOMAXPROCS(0).
	Resources registry.ResourceBudget
}

type transformPlan struct {
	id           string
	role         manifest.NodeType
	plugin       string
	config       registry.Configuration
	resources    registry.ResourceRequest
	input        media.StreamInfo
	output       media.StreamInfo
	autoInserted bool
	factory      func(registry.TransformFactoryOptions) (node.Node, error)
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

	demuxManifest := spec.DemuxManifest
	var err error
	if demuxManifest.Name == "" {
		demuxManifest, err = n.demuxerResolver.ResolveDemuxer(spec.Input)
		if err != nil {
			return nil, fmt.Errorf("resolve demuxer: %w", err)
		}
	}
	demuxConfig, err := configurationFor(demuxManifest, spec.DemuxConfig)
	if err != nil {
		return nil, fmt.Errorf("configure demuxer %s: %w", demuxManifest.Name, err)
	}
	demuxNode, err := demuxManifest.Factory(spec.Input, demuxConfig)
	if err != nil {
		return nil, fmt.Errorf("create demuxer: %w", err)
	}
	geometry := pipeline.NewGeometry()
	if err := geometry.AddNodeDef(pipeline.NodeDef{
		ID:   "demuxer",
		Node: demuxNode,
		Description: pipeline.NodeDescription{
			Role: manifest.RoleDemuxer, Plugin: demuxManifest.Name, Configuration: demuxConfig,
		},
	}); err != nil {
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
	if err := geometry.SetNodeDescription("demuxer", pipeline.NodeDescription{
		Role: manifest.RoleDemuxer, Plugin: demuxManifest.Name, Configuration: demuxConfig, Outputs: streams,
	}); err != nil {
		return nil, err
	}

	inputStream := streams[0]
	if spec.SelectInputStream != nil {
		inputStream, err = spec.SelectInputStream(streams)
		if err != nil {
			return nil, fmt.Errorf("select input stream: %w", err)
		}
	}

	plans := make([]transformPlan, 0, 2+len(spec.Filters))
	bridgeID := 0
	currentStream := inputStream

	decoderManifest := spec.DecoderManifest
	if decoderManifest.Name == "" {
		decoderManifest, err = n.decoderResolver.ResolveDecoder(currentStream)
		if err != nil {
			return nil, fmt.Errorf("resolve decoder: %w", err)
		}
	}
	decodeConfig, err := configurationFor(decoderManifest, spec.DecodeConfig)
	if err != nil {
		return nil, fmt.Errorf("configure decoder %s: %w", decoderManifest.Name, err)
	}
	if spec.DecoderManifest.Name != "" {
		accepted, err := decoderManifest.Accept(currentStream, currentStream.Codec, decodeConfig)
		if err != nil {
			return nil, fmt.Errorf("check decoder %s: %w", decoderManifest.Name, err)
		}
		if !accepted {
			return nil, fmt.Errorf("decoder %q does not accept input codec %q", decoderManifest.Name, currentStream.Codec)
		}
	}
	decoderOutput, err := transformStream(decoderManifest.TransformManifest, currentStream, currentStream.Codec, decodeConfig)
	if err != nil {
		return nil, fmt.Errorf("resolve decoder output stream: %w", err)
	}
	decoderInput := currentStream
	plans = append(plans, transformPlan{
		id:        "decoder",
		role:      manifest.RoleDecoder,
		plugin:    decoderManifest.Name,
		config:    decodeConfig,
		resources: decoderManifest.Resources,
		input:     decoderInput,
		output:    decoderOutput,
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
		requirements, requirementErr := filterManifest.Requirements(currentStream.Codec, filterSpec.Config)
		if requirementErr != nil {
			return nil, fmt.Errorf("resolve filter %d requirements: %w", i, requirementErr)
		}
		accepted := manifest.MatchesAny(requirements, currentStream)
		if !accepted {
			var bridgePlans []transformPlan
			currentStream, bridgePlans, err = n.satisfy(currentStream, requirements, &bridgeID)
			if err != nil {
				return nil, fmt.Errorf("satisfy filter %d (%s): %w", i, filterManifest.Name, err)
			}
			plans = append(plans, bridgePlans...)
		}
		filterOutput, err := transformStream(filterManifest.TransformManifest, currentStream, currentStream.Codec, filterSpec.Config)
		if err != nil {
			return nil, fmt.Errorf("resolve filter %d output stream: %w", i, err)
		}
		filterInput := currentStream
		resolvedManifest := filterManifest
		plans = append(plans, transformPlan{
			id:        fmt.Sprintf("filter:%d", i),
			role:      manifest.RoleFilter,
			plugin:    resolvedManifest.Name,
			config:    filterSpec.Config,
			resources: resolvedManifest.Resources,
			input:     filterInput,
			output:    filterOutput,
			factory: func(options registry.TransformFactoryOptions) (node.Node, error) {
				return resolvedManifest.Factory(filterInput, options)
			},
		})
		currentStream = filterOutput
	}

	encoderManifest := spec.EncoderManifest
	if encoderManifest.Name == "" {
		encoderManifest, err = n.encoderResolver.ResolveEncoder(spec.TargetCodec)
		if err != nil {
			return nil, fmt.Errorf("resolve encoder: %w", err)
		}
	} else if !encoderManifest.Supports(spec.TargetCodec) {
		return nil, fmt.Errorf("encoder %q does not support codec %q", encoderManifest.Name, spec.TargetCodec)
	}
	encodeConfig, err := configurationFor(encoderManifest, spec.EncodeConfig)
	if err != nil {
		return nil, fmt.Errorf("configure encoder %s: %w", encoderManifest.Name, err)
	}
	requirements, err := encoderManifest.Requirements(spec.TargetCodec, encodeConfig)
	if err != nil {
		return nil, fmt.Errorf("resolve encoder %s requirements: %w", encoderManifest.Name, err)
	}
	accepted := manifest.MatchesAny(requirements, currentStream)
	if !accepted {
		var bridgePlans []transformPlan
		currentStream, bridgePlans, err = n.satisfy(currentStream, requirements, &bridgeID)
		if err != nil {
			return nil, fmt.Errorf("satisfy encoder %s: %w", encoderManifest.Name, err)
		}
		plans = append(plans, bridgePlans...)
	}
	encoderOutput, err := transformStream(encoderManifest.TransformManifest, currentStream, spec.TargetCodec, encodeConfig)
	if err != nil {
		return nil, fmt.Errorf("resolve encoder output stream: %w", err)
	}
	encoderOutput.Codec = spec.TargetCodec
	encoderInput := currentStream
	plans = append(plans, transformPlan{
		id:        "encoder",
		role:      manifest.RoleEncoder,
		plugin:    encoderManifest.Name,
		config:    encodeConfig,
		resources: encoderManifest.Resources,
		input:     encoderInput,
		output:    encoderOutput,
		factory: func(options registry.TransformFactoryOptions) (node.Node, error) {
			return encoderManifest.Factory(encoderInput, spec.TargetCodec, options)
		},
	})

	muxManifest := spec.MuxManifest
	if muxManifest.Name == "" {
		muxManifest, err = n.muxerResolver.ResolveMuxer(spec.MuxConfig)
		if err != nil {
			return nil, fmt.Errorf("resolve muxer: %w", err)
		}
	}
	if !muxManifest.Supports(spec.TargetCodec) {
		return nil, fmt.Errorf("muxer %q does not support codec %q", muxManifest.Name, spec.TargetCodec)
	}
	muxConfig, err := configurationFor(muxManifest, spec.MuxConfig)
	if err != nil {
		return nil, fmt.Errorf("configure muxer %s: %w", muxManifest.Name, err)
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
		if err := geometry.AddNodeDef(pipeline.NodeDef{
			ID:   plan.id,
			Node: transformNodes[i],
			Description: pipeline.NodeDescription{
				Role:          plan.role,
				Plugin:        plan.plugin,
				Configuration: plan.config,
				Resources:     allocations[i],
				Inputs:        []media.StreamInfo{plan.input},
				Outputs:       []media.StreamInfo{plan.output},
				AutoInserted:  plan.autoInserted,
			},
		}); err != nil {
			return nil, fmt.Errorf("add %s to geometry: %w", plan.id, err)
		}
	}

	muxNode, err := muxManifest.Factory(spec.Output, muxConfig)
	if err != nil {
		return nil, fmt.Errorf("create muxer: %w", err)
	}
	if err := geometry.AddNodeDef(pipeline.NodeDef{
		ID:   "muxer",
		Node: muxNode,
		Description: pipeline.NodeDescription{
			Role: manifest.RoleMuxer, Plugin: muxManifest.Name, Configuration: muxConfig,
		},
	}); err != nil {
		return nil, fmt.Errorf("add muxer to geometry: %w", err)
	}
	if err := muxNode.SetMetadata(demuxNode.Metadata().Clone()); err != nil {
		return nil, fmt.Errorf("set muxer metadata: %w", err)
	}

	outputStream := encoderOutput
	if spec.PrepareOutputStream != nil {
		outputStream = spec.PrepareOutputStream(outputStream)
	}
	outputIndex, err := muxNode.AddStream(outputStream)
	if err != nil {
		return nil, fmt.Errorf("add output stream to muxer: %w", err)
	}
	outputStream.Index = outputIndex
	if err := geometry.SetNodeDescription("muxer", pipeline.NodeDescription{
		Role: manifest.RoleMuxer, Plugin: muxManifest.Name, Configuration: muxConfig, Inputs: []media.StreamInfo{outputStream},
	}); err != nil {
		return nil, err
	}

	previous := "demuxer"
	previousPort := "out"
	for i, plan := range plans {
		if err := geometry.AddEdgeDef(pipeline.EdgeDef{
			FromNode: previous, FromPort: previousPort, ToNode: plan.id, ToPort: "in",
			Stream: plan.input, ProgressSource: i == 0,
		}); err != nil {
			return nil, err
		}
		previous = plan.id
		previousPort = "out"
	}
	if err := geometry.AddEdgeDef(pipeline.EdgeDef{
		FromNode: previous, FromPort: previousPort, ToNode: "muxer", ToPort: "in", Stream: outputStream,
	}); err != nil {
		return nil, err
	}
	return geometry, nil
}

func configurationFor(manifest registry.Manifest, requested registry.Configuration) (registry.Configuration, error) {
	if requested == nil {
		if manifest.ID().ConfigurationType() == nil {
			return nil, nil
		}
		return manifest.NewConfiguration()
	}
	actual := reflect.TypeOf(requested)
	value := reflect.ValueOf(requested)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return nil, fmt.Errorf("configuration must not be a nil pointer")
	}
	for actual.Kind() == reflect.Pointer {
		actual = actual.Elem()
	}
	expected := manifest.ID().ConfigurationType()
	if expected == nil {
		return requested, nil
	}
	if actual != expected {
		return nil, fmt.Errorf("configuration type %s does not match %s", actual, manifest.ID())
	}
	return requested, nil
}

func transformStream(
	transform registry.TransformManifest,
	stream media.StreamInfo,
	target media.CodecID,
	config registry.Configuration,
) (media.StreamInfo, error) {
	return transform.TransformStream(stream, target, config)
}

func (n *Negotiator) satisfy(
	current media.StreamInfo,
	required []manifest.Capability,
	bridgeID *int,
) (media.StreamInfo, []transformPlan, error) {
	if manifest.MatchesAny(required, current) {
		return current, nil, nil
	}
	if n.bridgeResolver == nil {
		return current, nil, manifest.Diagnose(current, required)
	}
	steps, err := n.bridgeResolver.ResolveBridge(current, required)
	if err != nil {
		return current, nil, err
	}
	expected := current
	for i, step := range steps {
		if !reflect.DeepEqual(step.Input, expected) {
			return current, nil, fmt.Errorf("bridge step %d input does not match the preceding stream", i)
		}
		expected = step.Output
	}
	if !manifest.MatchesAny(required, expected) {
		return current, nil, fmt.Errorf("bridge resolver returned a plan that does not satisfy the required capability")
	}

	plans := make([]transformPlan, 0, len(steps))
	for _, step := range steps {
		step := step
		id := fmt.Sprintf("bridge:%d", *bridgeID)
		*bridgeID++
		plans = append(plans, transformPlan{
			id:           id,
			role:         manifest.RoleFilter,
			plugin:       step.Manifest.Name,
			config:       step.Config,
			resources:    step.Manifest.Resources,
			input:        step.Input,
			output:       step.Output,
			autoInserted: true,
			factory: func(options registry.TransformFactoryOptions) (node.Node, error) {
				return step.Manifest.Factory(step.Input, options)
			},
		})
	}
	return expected, plans, nil
}
