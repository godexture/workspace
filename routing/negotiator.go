package routing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"sort"

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
	Inputs map[string]string
}

type AuxInputSpec struct {
	Source io.ReadSeeker

	DemuxManifest registry.DemuxerManifest
	DemuxConfig   registry.Configuration

	DecoderManifest registry.DecoderManifest
	DecodeConfig    registry.Configuration
	Filters         []FilterSpec
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
	AuxInputs       map[string]AuxInputSpec

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
	if (len(spec.Filters) > 0 || auxFiltersRequested(spec.AuxInputs)) && n.filterResolver == nil {
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

	bridgeID := 0
	auxPaths, err := n.negotiateAuxPaths(ctx, spec.AuxInputs, geometry, &bridgeID)
	if err != nil {
		return nil, err
	}
	plans := make([]transformPlan, 0, 2+len(spec.Filters))
	filterPorts := make([]map[string]struct{}, 0, len(spec.Filters))
	filterManifests := make([]registry.FilterManifest, 0, len(spec.Filters))
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
		accepted, err := decoderManifest.Accept("in", currentStream, currentStream.Codec, decodeConfig)
		if err != nil {
			return nil, fmt.Errorf("check decoder %s: %w", decoderManifest.Name, err)
		}
		if !accepted {
			return nil, fmt.Errorf("decoder %q does not accept input codec %q", decoderManifest.Name, currentStream.Codec)
		}
	}
	decoderProbe, decoderOutput, err := decoderManifest.Factory(currentStream, registry.TransformFactoryOptions{Config: decodeConfig})
	if err != nil {
		return nil, fmt.Errorf("resolve decoder output stream: %w", err)
	}
	if err := decoderProbe.Close(); err != nil {
		return nil, fmt.Errorf("close decoder profile probe: %w", err)
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
			created, output, factoryErr := decoderManifest.Factory(decoderInput, options)
			if factoryErr != nil {
				return nil, factoryErr
			}
			if !reflect.DeepEqual(output, decoderOutput) {
				closeErr := created.Close()
				return nil, errors.Join(fmt.Errorf("decoder factory output differs from its profile probe"), closeErr)
			}
			return created, nil
		},
	})
	currentStream = decoderOutput

	for i, filterSpec := range spec.Filters {
		filterManifest, err := n.filterResolver.ResolveFilter(filterSpec.Config)
		if err != nil {
			return nil, fmt.Errorf("resolve filter %d: %w", i, err)
		}
		requirements, requirementErr := filterManifest.Requirements("in", currentStream.Codec, filterSpec.Config)
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
		filterProbe, filterOutput, err := filterManifest.Factory(currentStream, registry.TransformFactoryOptions{Config: filterSpec.Config})
		if err != nil {
			return nil, fmt.Errorf("resolve filter %d output stream: %w", i, err)
		}
		ports := make(map[string]struct{}, len(filterProbe.InputPorts()))
		for port := range filterProbe.InputPorts() {
			ports[port] = struct{}{}
		}
		if err := filterProbe.Close(); err != nil {
			return nil, fmt.Errorf("close filter %d profile probe: %w", i, err)
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
				created, output, factoryErr := resolvedManifest.Factory(filterInput, options)
				if factoryErr != nil {
					return nil, factoryErr
				}
				if !reflect.DeepEqual(output, filterOutput) {
					closeErr := created.Close()
					return nil, errors.Join(fmt.Errorf("filter factory output differs from its profile probe"), closeErr)
				}
				return created, nil
			},
		})
		filterPorts = append(filterPorts, ports)
		filterManifests = append(filterManifests, filterManifest)
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
	requirements, err := encoderManifest.Requirements("in", spec.TargetCodec, encodeConfig)
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
	encoderProbe, encoderOutput, err := encoderManifest.Factory(currentStream, spec.TargetCodec, registry.TransformFactoryOptions{Config: encodeConfig})
	if err != nil {
		return nil, fmt.Errorf("resolve encoder output stream: %w", err)
	}
	if err := encoderProbe.Close(); err != nil {
		return nil, fmt.Errorf("close encoder profile probe: %w", err)
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
			created, output, factoryErr := encoderManifest.Factory(encoderInput, spec.TargetCodec, options)
			if factoryErr != nil {
				return nil, factoryErr
			}
			if !reflect.DeepEqual(output, encoderOutput) {
				closeErr := created.Close()
				return nil, errors.Join(fmt.Errorf("encoder factory output differs from its profile probe"), closeErr)
			}
			return created, nil
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

	auxNames := make([]string, 0, len(auxPaths))
	for name := range auxPaths {
		auxNames = append(auxNames, name)
	}
	sort.Strings(auxNames)
	auxEdges := make([]pipeline.EdgeDef, 0)
	usedAux := make(map[string]struct{})
	for index, filterSpec := range spec.Filters {
		for port, name := range filterSpec.Inputs {
			if port == "in" {
				return nil, fmt.Errorf("filter %d input port in is reserved for the main stream", index)
			}
			path, ok := auxPaths[name]
			if !ok {
				return nil, fmt.Errorf("filter %d input port %q references unknown auxiliary input %q", index, port, name)
			}
			if _, used := usedAux[name]; used {
				return nil, fmt.Errorf("auxiliary input %q is connected more than once", name)
			}
			if _, exists := filterPorts[index][port]; !exists {
				return nil, fmt.Errorf("filter %d has no active input port %q", index, port)
			}
			filter := filterManifests[index]
			requirements, err := filter.Requirements(port, path.output.Codec, filterSpec.Config)
			if err != nil {
				return nil, fmt.Errorf("resolve filter %d port %q requirements: %w", index, port, err)
			}
			if !manifest.MatchesAny(requirements, path.output) {
				var bridges []transformPlan
				path.output, bridges, err = n.satisfy(path.output, requirements, &bridgeID)
				if err != nil {
					return nil, fmt.Errorf("satisfy auxiliary input %q for filter %d port %q: %w", name, index, port, err)
				}
				path.plans = append(path.plans, bridges...)
				if len(bridges) > 0 {
					path.tailID = bridges[len(bridges)-1].id
				}
			}
			usedAux[name] = struct{}{}
			auxEdges = append(auxEdges, pipeline.EdgeDef{FromNode: path.tailID, FromPort: "out", ToNode: fmt.Sprintf("filter:%d", index), ToPort: port, Stream: path.output})
		}
		for port := range filterPorts[index] {
			if port == "in" {
				continue
			}
			if _, connected := filterSpec.Inputs[port]; !connected {
				return nil, fmt.Errorf("filter %d input port %q requires an auxiliary input", index, port)
			}
		}
	}
	for _, name := range auxNames {
		if _, used := usedAux[name]; !used {
			return nil, fmt.Errorf("auxiliary input %q is not connected", name)
		}
	}

	allPlans := make([]transformPlan, 0, len(plans))
	for _, name := range auxNames {
		allPlans = append(allPlans, auxPaths[name].plans...)
	}
	allPlans = append(allPlans, plans...)

	parallelism := spec.Resources.Parallelism
	if parallelism == 0 {
		parallelism = runtime.GOMAXPROCS(0)
	}
	requests := make([]registry.ResourceRequest, len(allPlans))
	needsPool := false
	for i := range allPlans {
		requests[i] = allPlans[i].resources
		needsPool = needsPool || requests[i].Parallelism
	}

	// One pool is shared by every parallel-eligible stage for the whole
	// conversion, instead of splitting parallelism evenly across stages up
	// front: capacity then flows to whichever stage currently has runnable
	// work, rather than sitting idle in a stage with nothing to do.
	var pool *registry.WorkerPool
	if needsPool && parallelism > 1 {
		pool = registry.NewWorkerPool(parallelism)
		if err := geometry.AddResourceCloser(pool.Close); err != nil {
			return nil, fmt.Errorf("register resource pool: %w", err)
		}
	}
	grants := grantResources(requests, pool)

	transformNodes := make([]node.Node, len(allPlans))
	for i, plan := range allPlans {
		transformNodes[i], err = plan.factory(registry.TransformFactoryOptions{Config: plan.config})
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
				Resources:     grants[i],
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
	for _, name := range auxNames {
		path := auxPaths[name]
		previous := path.demuxID
		for _, plan := range path.plans {
			if err := geometry.AddEdgeDef(pipeline.EdgeDef{FromNode: previous, FromPort: "out", ToNode: plan.id, ToPort: "in", Stream: plan.input}); err != nil {
				return nil, err
			}
			previous = plan.id
		}
	}
	for _, edge := range auxEdges {
		if err := geometry.AddEdgeDef(edge); err != nil {
			return nil, err
		}
	}
	return geometry, nil
}

func auxFiltersRequested(inputs map[string]AuxInputSpec) bool {
	for _, input := range inputs {
		if len(input.Filters) > 0 {
			return true
		}
	}
	return false
}

type auxPath struct {
	name    string
	demuxID string
	plans   []transformPlan
	tailID  string
	output  media.StreamInfo
}

func (n *Negotiator) negotiateAuxPaths(ctx context.Context, inputs map[string]AuxInputSpec, geometry *pipeline.Geometry, bridgeID *int) (map[string]*auxPath, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	paths := make(map[string]*auxPath, len(inputs))
	for _, name := range names {
		if name == "" {
			return nil, fmt.Errorf("auxiliary input name must not be empty")
		}
		spec := inputs[name]
		if spec.Source == nil {
			return nil, fmt.Errorf("auxiliary input %q source must not be nil", name)
		}
		demux := spec.DemuxManifest
		var err error
		if demux.Name == "" {
			demux, err = n.demuxerResolver.ResolveDemuxer(spec.Source)
			if err != nil {
				return nil, fmt.Errorf("resolve auxiliary input %q demuxer: %w", name, err)
			}
		}
		demuxConfig, err := configurationFor(demux, spec.DemuxConfig)
		if err != nil {
			return nil, fmt.Errorf("configure auxiliary input %q demuxer: %w", name, err)
		}
		demuxNode, err := demux.Factory(spec.Source, demuxConfig)
		if err != nil {
			return nil, fmt.Errorf("create auxiliary input %q demuxer: %w", name, err)
		}
		demuxID := fmt.Sprintf("aux:%s:demuxer", name)
		if err := geometry.AddNodeDef(pipeline.NodeDef{ID: demuxID, Node: demuxNode, Description: pipeline.NodeDescription{Role: manifest.RoleDemuxer, Plugin: demux.Name, Configuration: demuxConfig}}); err != nil {
			return nil, err
		}
		streams, err := demuxNode.Streams()
		if err != nil {
			return nil, fmt.Errorf("get auxiliary input %q streams: %w", name, err)
		}
		if len(streams) == 0 {
			return nil, fmt.Errorf("auxiliary input %q has no streams", name)
		}
		if err := geometry.SetNodeDescription(demuxID, pipeline.NodeDescription{Role: manifest.RoleDemuxer, Plugin: demux.Name, Configuration: demuxConfig, Outputs: streams}); err != nil {
			return nil, err
		}
		current := streams[0]
		decoder := spec.DecoderManifest
		if decoder.Name == "" {
			decoder, err = n.decoderResolver.ResolveDecoder(current)
			if err != nil {
				return nil, fmt.Errorf("resolve auxiliary input %q decoder: %w", name, err)
			}
		}
		decodeConfig, err := configurationFor(decoder, spec.DecodeConfig)
		if err != nil {
			return nil, fmt.Errorf("configure auxiliary input %q decoder: %w", name, err)
		}
		if spec.DecoderManifest.Name != "" {
			accepted, err := decoder.Accept("in", current, current.Codec, decodeConfig)
			if err != nil {
				return nil, fmt.Errorf("check auxiliary input %q decoder: %w", name, err)
			}
			if !accepted {
				return nil, fmt.Errorf("auxiliary input %q decoder %q does not accept input codec %q", name, decoder.Name, current.Codec)
			}
		}
		probe, output, err := decoder.Factory(current, registry.TransformFactoryOptions{Config: decodeConfig})
		if err != nil {
			return nil, fmt.Errorf("resolve auxiliary input %q decoder output: %w", name, err)
		}
		if err := probe.Close(); err != nil {
			return nil, fmt.Errorf("close auxiliary input %q decoder probe: %w", name, err)
		}
		input := current
		plans := []transformPlan{{
			id: fmt.Sprintf("aux:%s:decoder", name), role: manifest.RoleDecoder, plugin: decoder.Name, config: decodeConfig, resources: decoder.Resources, input: input, output: output,
			factory: func(options registry.TransformFactoryOptions) (node.Node, error) {
				created, actual, err := decoder.Factory(input, options)
				if err != nil {
					return nil, err
				}
				if !reflect.DeepEqual(actual, output) {
					return nil, errors.Join(fmt.Errorf("auxiliary decoder factory output differs from its profile probe"), created.Close())
				}
				return created, nil
			},
		}}
		current = output
		for index, filterSpec := range spec.Filters {
			if len(filterSpec.Inputs) != 0 {
				return nil, fmt.Errorf("auxiliary input %q filter %d cannot have auxiliary inputs", name, index)
			}
			filter, err := n.filterResolver.ResolveFilter(filterSpec.Config)
			if err != nil {
				return nil, fmt.Errorf("resolve auxiliary input %q filter %d: %w", name, index, err)
			}
			requirements, err := filter.Requirements("in", current.Codec, filterSpec.Config)
			if err != nil {
				return nil, fmt.Errorf("resolve auxiliary input %q filter %d requirements: %w", name, index, err)
			}
			if !manifest.MatchesAny(requirements, current) {
				var bridges []transformPlan
				current, bridges, err = n.satisfy(current, requirements, bridgeID)
				if err != nil {
					return nil, fmt.Errorf("satisfy auxiliary input %q filter %d: %w", name, index, err)
				}
				plans = append(plans, bridges...)
			}
			filterInput := current
			probe, filterOutput, err := filter.Factory(filterInput, registry.TransformFactoryOptions{Config: filterSpec.Config})
			if err != nil {
				return nil, fmt.Errorf("resolve auxiliary input %q filter %d output: %w", name, index, err)
			}
			if err := probe.Close(); err != nil {
				return nil, fmt.Errorf("close auxiliary input %q filter %d probe: %w", name, index, err)
			}
			resolved := filter
			plans = append(plans, transformPlan{
				id: fmt.Sprintf("aux:%s:filter:%d", name, index), role: manifest.RoleFilter, plugin: resolved.Name, config: filterSpec.Config, resources: resolved.Resources, input: filterInput, output: filterOutput,
				factory: func(options registry.TransformFactoryOptions) (node.Node, error) {
					created, actual, err := resolved.Factory(filterInput, options)
					if err != nil {
						return nil, err
					}
					if !reflect.DeepEqual(actual, filterOutput) {
						return nil, errors.Join(fmt.Errorf("auxiliary filter factory output differs from its profile probe"), created.Close())
					}
					return created, nil
				},
			})
			current = filterOutput
		}
		paths[name] = &auxPath{name: name, demuxID: demuxID, plans: plans, tailID: plans[len(plans)-1].id, output: current}
	}
	return paths, nil
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
				created, output, factoryErr := step.Manifest.Factory(step.Input, options)
				if factoryErr != nil {
					return nil, factoryErr
				}
				if !reflect.DeepEqual(output, step.Output) {
					closeErr := created.Close()
					return nil, errors.Join(fmt.Errorf("bridge factory output differs from its profile probe"), closeErr)
				}
				return created, nil
			},
		})
	}
	return expected, plans, nil
}
