package routing

import (
	"context"
	"fmt"
	"sort"

	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/core/registry"
)

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
