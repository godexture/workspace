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

// MainInputAlias is the reserved source alias for the main input's decoded
// stream. It cannot be used as an auxiliary input name or a filter alias.
const MainInputAlias = "@in"

// PortRef names one port of one graph node: a filter alias, an auxiliary
// input name, or MainInputAlias. An empty Port defaults to "out".
type PortRef struct {
	Alias string
	Port  string
}

// FilterSpec describes one filter node in the conversion graph. Inputs maps
// each port this filter's manifest requires to where it reads from. A port
// left out of Inputs falls back to a default only when it is literally named
// "in": the first filter in declaration order reads MainInputAlias, and
// every later one reads the "out" port of the filter declared immediately
// before it. Every other port (multi-port filters have no "in" at all) must
// be wired explicitly.
type FilterSpec struct {
	Alias  string
	Config registry.Configuration
	Inputs map[string]PortRef
	// Manifest is the filter's already-resolved manifest, when the caller
	// has one (as pkg/conversion does, since it must resolve a
	// parameterized filter's manifest from its Parameters before Config
	// even makes sense). A parameterized filter's concrete manifest exists
	// only for this one invocation, so it cannot be found again by
	// Negotiator's config-keyed filter resolver the way an ordinary
	// filter's can — leave this zero to fall back to that resolver.
	Manifest registry.FilterManifest
}

// AuxInputSpec is a named additional source, demuxed and decoded exactly
// like the main input. Filters read it by wiring a port to PortRef{Alias:
// name}; it has no filter chain of its own — any processing on the way to a
// consumer is just an ordinary FilterSpec wired from this alias, unified
// with the rest of the graph.
type AuxInputSpec struct {
	Source io.ReadSeeker

	DemuxManifest registry.DemuxerManifest
	DemuxConfig   registry.Configuration

	DecoderManifest registry.DecoderManifest
	DecodeConfig    registry.Configuration
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

	// Sink names the port that feeds the encoder. Nil resolves to the
	// default: the last filter's "out" port, or (with no filters) the main
	// input directly.
	Sink *PortRef

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
	inputs       media.StreamSet
	outputs      media.StreamSet
	autoInserted bool
	node         node.Node
}

// resolvedSource is where one filter input port reads from: a concrete
// (node ID, port) pair, plus which filter (if any) produces it, so the
// negotiator can order construction by dependency instead of declaration.
type resolvedSource struct {
	nodeID      string
	port        string
	filterIndex int // -1 when the source is not a filter (main input or auxiliary input)
}

func (n *Negotiator) NegotiateConversion(ctx context.Context, spec ConversionSpec) (result *pipeline.Geometry, resultErr error) {
	if ctx == nil {
		return nil, fmt.Errorf("context must not be nil")
	}
	if n.decoderResolver == nil || n.encoderResolver == nil || n.demuxerResolver == nil || n.muxerResolver == nil {
		return nil, fmt.Errorf("muxer, demuxer, encoder, and decoder resolvers must be provided")
	}
	needsFilterResolver := false
	for _, filterSpec := range spec.Filters {
		if filterSpec.Manifest.Factory == nil {
			needsFilterResolver = true
			break
		}
	}
	if needsFilterResolver && n.filterResolver == nil {
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
	ownedNodes := make([]node.Node, 0)
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
			resultErr = errors.Join(resultErr, closeOwnedNodes(ownedNodes))
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

	var graphEdges []pipeline.EdgeDef
	var allPlans []transformPlan

	auxSources, auxPlans, err := n.negotiateAuxSources(ctx, spec.AuxInputs, geometry, &ownedNodes, &graphEdges)
	if err != nil {
		return nil, err
	}
	allPlans = append(allPlans, auxPlans...)

	decoderManifest := spec.DecoderManifest
	if decoderManifest.Name == "" {
		decoderManifest, err = n.decoderResolver.ResolveDecoder(inputStream)
		if err != nil {
			return nil, fmt.Errorf("resolve decoder: %w", err)
		}
	}
	decodeConfig, err := configurationFor(decoderManifest, spec.DecodeConfig)
	if err != nil {
		return nil, fmt.Errorf("configure decoder %s: %w", decoderManifest.Name, err)
	}
	if spec.DecoderManifest.Name != "" {
		accepted, err := decoderManifest.Accept("in", inputStream, inputStream.Codec, decodeConfig)
		if err != nil {
			return nil, fmt.Errorf("check decoder %s: %w", decoderManifest.Name, err)
		}
		if !accepted {
			return nil, fmt.Errorf("decoder %q does not accept input codec %q", decoderManifest.Name, inputStream.Codec)
		}
	}
	decoderNode, decoderOutput, err := decoderManifest.Factory(inputStream, registry.TransformFactoryOptions{Config: decodeConfig})
	if err != nil {
		return nil, fmt.Errorf("resolve decoder output stream: %w", err)
	}
	ownedNodes = append(ownedNodes, decoderNode)
	allPlans = append(allPlans, transformPlan{
		id: "decoder", role: manifest.RoleDecoder, plugin: decoderManifest.Name, config: decodeConfig, resources: decoderManifest.Resources,
		inputs: media.StreamSet{"in": inputStream}, outputs: media.StreamSet{"out": decoderOutput}, node: decoderNode,
	})
	graphEdges = append(graphEdges, pipeline.EdgeDef{FromNode: "demuxer", FromPort: "out", ToNode: "decoder", ToPort: "in", Stream: inputStream, ProgressSource: true})

	resolvedOutputs := map[string]media.StreamSet{"decoder": {"out": decoderOutput}}
	for _, source := range auxSources {
		resolvedOutputs[source.decoderID] = media.StreamSet{"out": source.output}
	}

	aliasIndex := make(map[string]int, len(spec.Filters))
	for i, f := range spec.Filters {
		if f.Alias == "" {
			continue
		}
		if f.Alias == MainInputAlias {
			return nil, fmt.Errorf("filter %d alias %q is reserved", i, f.Alias)
		}
		if _, exists := spec.AuxInputs[f.Alias]; exists {
			return nil, fmt.Errorf("filter %d alias %q duplicates an auxiliary input name", i, f.Alias)
		}
		if _, exists := aliasIndex[f.Alias]; exists {
			return nil, fmt.Errorf("duplicate filter alias %q", f.Alias)
		}
		aliasIndex[f.Alias] = i
	}

	filterManifests := make([]registry.FilterManifest, len(spec.Filters))
	for i, filterSpec := range spec.Filters {
		if filterSpec.Manifest.Factory != nil {
			filterManifests[i] = filterSpec.Manifest
			continue
		}
		filterManifests[i], err = n.filterResolver.ResolveFilter(filterSpec.Config)
		if err != nil {
			return nil, fmt.Errorf("resolve filter %d: %w", i, err)
		}
	}

	type portSource struct {
		port   string
		source resolvedSource
	}
	filterSources := make([][]portSource, len(spec.Filters))
	inDegree := make([]int, len(spec.Filters))
	dependents := make([][]int, len(spec.Filters))
	for i, filterSpec := range spec.Filters {
		nodeID := filterID("", i, filterSpec.Alias)
		ports := requiredPorts(filterManifests[i].TransformManifest)
		filterSources[i] = make([]portSource, 0, len(ports))
		for _, port := range ports {
			var source resolvedSource
			if ref, explicit := filterSpec.Inputs[port]; explicit {
				source, err = resolveGraphSource(ref, aliasIndex, spec.AuxInputs)
				if err != nil {
					return nil, fmt.Errorf("filter %d (%s) port %q: %w", i, nodeID, port, err)
				}
			} else if port == "in" {
				source = defaultBackboneSource(i, spec.Filters)
			} else {
				return nil, fmt.Errorf("filter %d (%s) input port %q requires a wire", i, nodeID, port)
			}
			filterSources[i] = append(filterSources[i], portSource{port: port, source: source})
			if source.filterIndex >= 0 {
				inDegree[i]++
				dependents[source.filterIndex] = append(dependents[source.filterIndex], i)
			}
		}
	}
	order, err := topologicalOrder(inDegree, dependents)
	if err != nil {
		return nil, err
	}

	bridgeID := 0
	usedSources := make(map[string]bool)
	for _, i := range order {
		filterSpec := spec.Filters[i]
		fm := filterManifests[i]
		nodeID := filterID("", i, filterSpec.Alias)
		inputSet := make(media.StreamSet, len(filterSources[i]))
		for _, ps := range filterSources[i] {
			port, source := ps.port, ps.source
			key := source.nodeID + "\x00" + source.port
			if usedSources[key] {
				return nil, fmt.Errorf("filter %d (%s) port %q: source %s.%s is already wired elsewhere", i, nodeID, port, source.nodeID, source.port)
			}
			usedSources[key] = true
			sourceStream, ok := resolvedOutputs[source.nodeID][source.port]
			if !ok {
				return nil, fmt.Errorf("filter %d (%s) port %q: source %q has no output port %q", i, nodeID, port, source.nodeID, source.port)
			}
			requirements, err := fm.RequirementsFor(port, inputSet, sourceStream.Codec, filterSpec.Config)
			if err != nil {
				return nil, fmt.Errorf("resolve filter %d (%s) port %q requirements: %w", i, nodeID, port, err)
			}
			final, err := n.resolveEdge(source, requirements, nodeID, port, sourceStream, &bridgeID, &ownedNodes, &allPlans, &graphEdges)
			if err != nil {
				return nil, fmt.Errorf("satisfy filter %d (%s) port %q: %w", i, nodeID, port, err)
			}
			inputSet[port] = final
		}
		filterNode, outputSet, err := fm.Factory(inputSet, registry.TransformFactoryOptions{Config: filterSpec.Config})
		if err != nil {
			return nil, fmt.Errorf("resolve filter %d (%s) output streams: %w", i, nodeID, err)
		}
		ownedNodes = append(ownedNodes, filterNode)
		resolvedOutputs[nodeID] = outputSet
		allPlans = append(allPlans, transformPlan{
			id: nodeID, role: manifest.RoleFilter, plugin: fm.Name, config: filterSpec.Config, resources: fm.Resources,
			inputs: inputSet, outputs: outputSet, node: filterNode,
		})
	}

	var sink resolvedSource
	if spec.Sink != nil {
		sink, err = resolveGraphSource(*spec.Sink, aliasIndex, spec.AuxInputs)
		if err != nil {
			return nil, fmt.Errorf("sink: %w", err)
		}
	} else if len(spec.Filters) > 0 {
		sink = defaultBackboneSource(len(spec.Filters), spec.Filters)
	} else {
		sink = resolvedSource{nodeID: "decoder", port: "out", filterIndex: -1}
	}
	sinkStream, ok := resolvedOutputs[sink.nodeID][sink.port]
	if !ok {
		return nil, fmt.Errorf("output: source %q has no output port %q; set Sink explicitly", sink.nodeID, sink.port)
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
	encoderInput, err := n.resolveEdge(sink, requirements, "encoder", "in", sinkStream, &bridgeID, &ownedNodes, &allPlans, &graphEdges)
	if err != nil {
		return nil, fmt.Errorf("satisfy encoder %s: %w", encoderManifest.Name, err)
	}
	encoderNode, encoderOutput, err := encoderManifest.Factory(encoderInput, spec.TargetCodec, registry.TransformFactoryOptions{Config: encodeConfig})
	if err != nil {
		return nil, fmt.Errorf("resolve encoder output stream: %w", err)
	}
	ownedNodes = append(ownedNodes, encoderNode)
	encoderOutput.Codec = spec.TargetCodec
	allPlans = append(allPlans, transformPlan{
		id: "encoder", role: manifest.RoleEncoder, plugin: encoderManifest.Name, config: encodeConfig, resources: encoderManifest.Resources,
		inputs: media.StreamSet{"in": encoderInput}, outputs: media.StreamSet{"out": encoderOutput}, node: encoderNode,
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

	for i, plan := range allPlans {
		if err := geometry.AddNodeDef(pipeline.NodeDef{
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
			return nil, fmt.Errorf("add %s to geometry: %w", plan.id, err)
		}
		ownedNodes = releaseOwnedNode(ownedNodes, plan.node)
	}

	muxNode, err := muxManifest.Factory(spec.Output, muxConfig)
	if err != nil {
		return nil, fmt.Errorf("create muxer: %w", err)
	}
	ownedNodes = append(ownedNodes, muxNode)
	if err := geometry.AddNodeDef(pipeline.NodeDef{
		ID:   "muxer",
		Node: muxNode,
		Description: pipeline.NodeDescription{
			Role: manifest.RoleMuxer, Plugin: muxManifest.Name, Configuration: muxConfig,
		},
	}); err != nil {
		return nil, fmt.Errorf("add muxer to geometry: %w", err)
	}
	ownedNodes = releaseOwnedNode(ownedNodes, muxNode)
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
	graphEdges = append(graphEdges, pipeline.EdgeDef{FromNode: "encoder", FromPort: "out", ToNode: "muxer", ToPort: "in", Stream: outputStream})

	for _, edge := range graphEdges {
		if err := geometry.AddEdgeDef(edge); err != nil {
			return nil, err
		}
	}
	return geometry, nil
}

// resolveEdge negotiates one edge from a resolved source into a specific
// destination port, splicing in bridge nodes when the source stream does not
// already satisfy requirements. It generalizes what satisfy used to do only
// for the linear main chain to every edge in the graph.
func (n *Negotiator) resolveEdge(
	source resolvedSource,
	requirements []manifest.Capability,
	toNode, toPort string,
	sourceStream media.StreamInfo,
	bridgeID *int,
	ownedNodes *[]node.Node,
	allPlans *[]transformPlan,
	graphEdges *[]pipeline.EdgeDef,
) (media.StreamInfo, error) {
	final, bridgePlans, err := n.satisfy(sourceStream, requirements, bridgeID)
	if err != nil {
		return media.StreamInfo{}, err
	}
	*ownedNodes = appendPlanNodes(*ownedNodes, bridgePlans)
	*allPlans = append(*allPlans, bridgePlans...)
	from, fromPort := source.nodeID, source.port
	for _, bridgePlan := range bridgePlans {
		*graphEdges = append(*graphEdges, pipeline.EdgeDef{FromNode: from, FromPort: fromPort, ToNode: bridgePlan.id, ToPort: "in", Stream: bridgePlan.inputs["in"]})
		from, fromPort = bridgePlan.id, "out"
	}
	*graphEdges = append(*graphEdges, pipeline.EdgeDef{FromNode: from, FromPort: fromPort, ToNode: toNode, ToPort: toPort, Stream: final})
	return final, nil
}

// requiredPorts lists a filter's declared input ports, "in" first (so a
// port's ProfileRequirements can depend on "in" having already been
// resolved) and the rest in a stable, deterministic order.
func requiredPorts(m registry.TransformManifest) []string {
	ports := make([]string, 0, len(m.InputRequirements))
	hasIn := false
	for port := range m.InputRequirements {
		if port == "in" {
			hasIn = true
			continue
		}
		ports = append(ports, port)
	}
	sort.Strings(ports)
	if hasIn {
		ports = append([]string{"in"}, ports...)
	}
	return ports
}

// defaultBackboneSource is the implicit source for a filter's "in" port when
// nothing wires it explicitly. The backbone is the subsequence of filters
// whose own "in" port is not explicitly wired — a filter with an explicit
// "in" (e.g. one reading an auxiliary input) sits outside it entirely, the
// same way it used to be excluded from the old main filter chain, so it is
// skipped both as a consumer of the default and as a candidate predecessor.
// The source is the nearest such filter declared before index, or the main
// input if there is none. index may equal len(filters), meaning "after the
// last filter" (used to resolve the default output sink).
func defaultBackboneSource(index int, filters []FilterSpec) resolvedSource {
	for j := index - 1; j >= 0; j-- {
		if _, explicit := filters[j].Inputs["in"]; explicit {
			continue
		}
		return resolvedSource{nodeID: filterID("", j, filters[j].Alias), port: "out", filterIndex: j}
	}
	return resolvedSource{nodeID: "decoder", port: "out", filterIndex: -1}
}

// resolveGraphSource turns an explicit PortRef into a concrete node
// reference. Only aliased filters are addressable this way; an unaliased
// filter can still receive a port by declaration-order default, but nothing
// else can wire from its output.
func resolveGraphSource(ref PortRef, aliasIndex map[string]int, aux map[string]AuxInputSpec) (resolvedSource, error) {
	if ref.Alias == "" {
		return resolvedSource{}, fmt.Errorf("source alias must not be empty")
	}
	port := ref.Port
	if port == "" {
		port = "out"
	}
	if ref.Alias == MainInputAlias {
		return resolvedSource{nodeID: "decoder", port: port, filterIndex: -1}, nil
	}
	if _, ok := aux[ref.Alias]; ok {
		return resolvedSource{nodeID: fmt.Sprintf("aux:%s:decoder", ref.Alias), port: port, filterIndex: -1}, nil
	}
	if idx, ok := aliasIndex[ref.Alias]; ok {
		return resolvedSource{nodeID: "filter:" + ref.Alias, port: port, filterIndex: idx}, nil
	}
	return resolvedSource{}, fmt.Errorf("unknown source alias %q", ref.Alias)
}

// topologicalOrder returns filter indices in dependency order (a filter
// always comes after every other filter its inputs are wired from),
// preferring the lowest not-yet-ready index at each step so that a graph
// with no cross-order dependency reproduces plain declaration order.
func topologicalOrder(inDegree []int, dependents [][]int) ([]int, error) {
	remaining := append([]int(nil), inDegree...)
	done := make([]bool, len(inDegree))
	order := make([]int, 0, len(inDegree))
	for len(order) < len(inDegree) {
		next := -1
		for i, isDone := range done {
			if !isDone && remaining[i] == 0 {
				next = i
				break
			}
		}
		if next == -1 {
			return nil, fmt.Errorf("filter graph has a cycle")
		}
		done[next] = true
		order = append(order, next)
		for _, dependent := range dependents[next] {
			remaining[dependent]--
		}
	}
	return order, nil
}

func streamValues(set media.StreamSet) []media.StreamInfo {
	if len(set) == 0 {
		return nil
	}
	ports := make([]string, 0, len(set))
	for port := range set {
		ports = append(ports, port)
	}
	sort.Strings(ports)
	values := make([]media.StreamInfo, len(ports))
	for i, port := range ports {
		values[i] = set[port]
	}
	return values
}

func filterID(auxiliary string, index int, alias string) string {
	name := fmt.Sprintf("%d", index)
	if alias != "" {
		name = alias
	}
	if auxiliary == "" {
		return "filter:" + name
	}
	return fmt.Sprintf("aux:%s:filter:%s", auxiliary, name)
}

type auxSource struct {
	decoderID string
	output    media.StreamInfo
}

// negotiateAuxSources demuxes and decodes every named auxiliary input, in
// name order. It only builds the source node itself; any filter chain
// processing an auxiliary input on its way to a consumer is just an
// ordinary FilterSpec wired from its alias, resolved later alongside every
// other filter.
func (n *Negotiator) negotiateAuxSources(ctx context.Context, inputs map[string]AuxInputSpec, geometry *pipeline.Geometry, ownedNodes *[]node.Node, graphEdges *[]pipeline.EdgeDef) (map[string]*auxSource, []transformPlan, error) {
	if len(inputs) == 0 {
		return nil, nil, nil
	}
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)

	sources := make(map[string]*auxSource, len(inputs))
	plans := make([]transformPlan, 0, len(inputs))
	for _, name := range names {
		if name == "" {
			return nil, nil, fmt.Errorf("auxiliary input name must not be empty")
		}
		if name == MainInputAlias {
			return nil, nil, fmt.Errorf("auxiliary input name %q is reserved", name)
		}
		spec := inputs[name]
		if spec.Source == nil {
			return nil, nil, fmt.Errorf("auxiliary input %q source must not be nil", name)
		}
		demux := spec.DemuxManifest
		var err error
		if demux.Name == "" {
			demux, err = n.demuxerResolver.ResolveDemuxer(spec.Source)
			if err != nil {
				return nil, nil, fmt.Errorf("resolve auxiliary input %q demuxer: %w", name, err)
			}
		}
		demuxConfig, err := configurationFor(demux, spec.DemuxConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("configure auxiliary input %q demuxer: %w", name, err)
		}
		demuxNode, err := demux.Factory(spec.Source, demuxConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("create auxiliary input %q demuxer: %w", name, err)
		}
		demuxID := fmt.Sprintf("aux:%s:demuxer", name)
		if err := geometry.AddNodeDef(pipeline.NodeDef{ID: demuxID, Node: demuxNode, Description: pipeline.NodeDescription{Role: manifest.RoleDemuxer, Plugin: demux.Name, Configuration: demuxConfig}}); err != nil {
			return nil, nil, err
		}
		*ownedNodes = append(*ownedNodes, demuxNode)
		streams, err := demuxNode.Streams()
		if err != nil {
			return nil, nil, fmt.Errorf("get auxiliary input %q streams: %w", name, err)
		}
		if len(streams) == 0 {
			return nil, nil, fmt.Errorf("auxiliary input %q has no streams", name)
		}
		if err := geometry.SetNodeDescription(demuxID, pipeline.NodeDescription{Role: manifest.RoleDemuxer, Plugin: demux.Name, Configuration: demuxConfig, Outputs: streams}); err != nil {
			return nil, nil, err
		}
		current := streams[0]
		decoder := spec.DecoderManifest
		if decoder.Name == "" {
			decoder, err = n.decoderResolver.ResolveDecoder(current)
			if err != nil {
				return nil, nil, fmt.Errorf("resolve auxiliary input %q decoder: %w", name, err)
			}
		}
		decodeConfig, err := configurationFor(decoder, spec.DecodeConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("configure auxiliary input %q decoder: %w", name, err)
		}
		if spec.DecoderManifest.Name != "" {
			accepted, err := decoder.Accept("in", current, current.Codec, decodeConfig)
			if err != nil {
				return nil, nil, fmt.Errorf("check auxiliary input %q decoder: %w", name, err)
			}
			if !accepted {
				return nil, nil, fmt.Errorf("auxiliary input %q decoder %q does not accept input codec %q", name, decoder.Name, current.Codec)
			}
		}
		decoderNode, output, err := decoder.Factory(current, registry.TransformFactoryOptions{Config: decodeConfig})
		if err != nil {
			return nil, nil, fmt.Errorf("resolve auxiliary input %q decoder output: %w", name, err)
		}
		*ownedNodes = append(*ownedNodes, decoderNode)
		decoderID := fmt.Sprintf("aux:%s:decoder", name)
		plans = append(plans, transformPlan{
			id: decoderID, role: manifest.RoleDecoder, plugin: decoder.Name, config: decodeConfig, resources: decoder.Resources,
			inputs: media.StreamSet{"in": current}, outputs: media.StreamSet{"out": output}, node: decoderNode,
		})
		*graphEdges = append(*graphEdges, pipeline.EdgeDef{FromNode: demuxID, FromPort: "out", ToNode: decoderID, ToPort: "in", Stream: current})
		sources[name] = &auxSource{decoderID: decoderID, output: output}
	}
	return sources, plans, nil
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
		created, outputs, factoryErr := step.Manifest.Factory(media.StreamSet{"in": step.Input}, registry.TransformFactoryOptions{Config: step.Config})
		if factoryErr != nil {
			return current, nil, factoryErr
		}
		output := outputs["out"]
		if !reflect.DeepEqual(output, step.Output) {
			return current, nil, errors.Join(fmt.Errorf("bridge factory output differs from the resolved bridge step"), created.Close())
		}
		plans = append(plans, transformPlan{
			id:           id,
			role:         manifest.RoleFilter,
			plugin:       step.Manifest.Name,
			config:       step.Config,
			resources:    step.Manifest.Resources,
			inputs:       media.StreamSet{"in": step.Input},
			outputs:      media.StreamSet{"out": step.Output},
			autoInserted: true,
			node:         created,
		})
	}
	return expected, plans, nil
}

func appendPlanNodes(owned []node.Node, plans []transformPlan) []node.Node {
	for _, plan := range plans {
		owned = append(owned, plan.node)
	}
	return owned
}

func releaseOwnedNode(owned []node.Node, target node.Node) []node.Node {
	for i, current := range owned {
		if sameNode(current, target) {
			return append(owned[:i], owned[i+1:]...)
		}
	}
	return owned
}

func sameNode(first, second node.Node) bool {
	if first == nil || second == nil {
		return first == second
	}
	firstValue := reflect.ValueOf(first)
	secondValue := reflect.ValueOf(second)
	if firstValue.Type() != secondValue.Type() || !firstValue.Type().Comparable() {
		return false
	}
	return firstValue.Interface() == secondValue.Interface()
}

func closeOwnedNodes(nodes []node.Node) error {
	var result error
	for i := len(nodes) - 1; i >= 0; i-- {
		if nodes[i] == nil {
			continue
		}
		if err := nodes[i].Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close unowned node %d (%T): %w", i, nodes[i], err))
		}
	}
	return result
}
