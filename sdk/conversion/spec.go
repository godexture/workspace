package conversion

import (
	"context"
	"fmt"
	"io"

	godec "github.com/godexture/godec/core"
	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/core/routing"
)

type PluginSpec struct {
	Name   string            `json:"name"`
	Values map[string]string `json:"values,omitempty"`
}

// PortRef names one port of one graph node: a filter alias, an auxiliary
// input name, or MainInputAlias. An empty Port defaults to "out".
type PortRef struct {
	Alias string `json:"alias"`
	Port  string `json:"port,omitempty"`
}

// MainInputAlias is the reserved source alias for the main input's decoded
// stream (see routing.MainInputAlias).
const MainInputAlias = routing.MainInputAlias

type FilterSpec struct {
	PluginSpec
	Alias      string             `json:"alias,omitempty"`
	Inputs     map[string]PortRef `json:"inputs,omitempty"`
	Parameters map[string]string  `json:"parameters,omitempty"`
}

// AuxInputSpec is a named additional source, demuxed and decoded like the
// main input. It has no filter chain of its own — any processing on the way
// to a consumer is just an ordinary FilterSpec wired from this alias.
type AuxInputSpec struct {
	Demuxer *PluginSpec `json:"demuxer,omitempty"`
	Decoder *PluginSpec `json:"decoder,omitempty"`
}

type InputSet struct {
	Main io.ReadSeeker
	Aux  map[string]io.ReadSeeker
}

type Spec struct {
	Demuxer   *PluginSpec             `json:"demuxer,omitempty"`
	Decoder   *PluginSpec             `json:"decoder,omitempty"`
	Filters   []FilterSpec            `json:"filters,omitempty"`
	AuxInputs map[string]AuxInputSpec `json:"auxInputs,omitempty"`
	// Sink names the port that feeds the encoder. nil resolves to the
	// default: the last filter's "out" port, or (with no filters) the main
	// input directly.
	Sink        *PortRef    `json:"sink,omitempty"`
	Codec       string      `json:"codec,omitempty"`
	Encoder     *PluginSpec `json:"encoder,omitempty"`
	Muxer       PluginSpec  `json:"muxer"`
	Parallelism int         `json:"parallelism,omitempty"`
}

// PlaybackSpec describes the input and filter graph for a decoded-frame
// playback pipeline. It deliberately has no encoder or muxer settings.
type PlaybackSpec struct {
	Demuxer     *PluginSpec             `json:"demuxer,omitempty"`
	Decoder     *PluginSpec             `json:"decoder,omitempty"`
	Filters     []FilterSpec            `json:"filters,omitempty"`
	AuxInputs   map[string]AuxInputSpec `json:"auxInputs,omitempty"`
	Sink        *PortRef                `json:"sink,omitempty"`
	Parallelism int                     `json:"parallelism,omitempty"`
}

type PlaybackSink struct {
	Name         string
	Requirements []manifest.Capability
	Factory      func(media.StreamInfo) (node.Sink, error)
}

type Resolved struct {
	Demuxer      registry.DemuxerManifest
	DemuxConfig  registry.Configuration
	Decoder      registry.DecoderManifest
	DecodeConfig registry.Configuration
	Filters      []routing.FilterSpec
	AuxInputs    map[string]resolvedAuxInput
	Sink         *routing.PortRef
	Encoder      registry.EncoderManifest
	EncodeConfig registry.Configuration
	Muxer        registry.MuxerManifest
	MuxConfig    registry.Configuration
	Codec        media.CodecID
	Resources    registry.ResourceBudget
}

type resolvedAuxInput struct {
	Demuxer      registry.DemuxerManifest
	DemuxConfig  registry.Configuration
	Decoder      registry.DecoderManifest
	DecodeConfig registry.Configuration
}

func Negotiate(ctx context.Context, inputs InputSet, output io.Writer, spec Spec) (*pipeline.Geometry, error) {
	if inputs.Main == nil {
		return nil, invalidSpec("main input is required")
	}
	resolved, err := Resolve(spec)
	if err != nil {
		return nil, err
	}
	aux, err := resolveAuxiliaryInputs(inputs, resolved.AuxInputs)
	if err != nil {
		return nil, err
	}
	geometry, err := godec.NewNegotiator().NegotiateConversion(ctx, routing.ConversionSpec{
		Input: inputs.Main, Output: output,
		DemuxManifest: resolved.Demuxer, DemuxConfig: resolved.DemuxConfig,
		DecoderManifest: resolved.Decoder, DecodeConfig: resolved.DecodeConfig,
		Filters:         resolved.Filters,
		AuxInputs:       aux,
		Sink:            resolved.Sink,
		EncoderManifest: resolved.Encoder, TargetCodec: resolved.Codec, EncodeConfig: resolved.EncodeConfig,
		MuxManifest: resolved.Muxer, MuxConfig: resolved.MuxConfig,
		Resources: resolved.Resources,
	})
	if err != nil {
		return nil, wrapError(CodeNegotiationFailed, "negotiate pipeline", err)
	}
	return geometry, nil
}

func NegotiatePlayback(ctx context.Context, inputs InputSet, spec PlaybackSpec, sink PlaybackSink) (*pipeline.Geometry, error) {
	if inputs.Main == nil {
		return nil, invalidSpec("main input is required")
	}
	if len(sink.Requirements) == 0 {
		return nil, invalidSpec("playback sink requirements are required")
	}
	if sink.Factory == nil {
		return nil, invalidSpec("playback sink factory is required")
	}
	resolved, err := resolveInput(spec)
	if err != nil {
		return nil, err
	}
	aux, err := resolveAuxiliaryInputs(inputs, resolved.AuxInputs)
	if err != nil {
		return nil, err
	}
	geometry, err := godec.NewNegotiator().NegotiatePlayback(ctx, routing.PlaybackSpec{
		Input:         inputs.Main,
		DemuxManifest: resolved.Demuxer, DemuxConfig: resolved.DemuxConfig,
		DecoderManifest: resolved.Decoder, DecodeConfig: resolved.DecodeConfig,
		Filters: resolved.Filters, AuxInputs: aux, Sink: resolved.Sink,
		SinkRequirements: sink.Requirements, SinkFactory: sink.Factory, SinkName: sink.Name,
		Resources: resolved.Resources,
	})
	if err != nil {
		return nil, wrapError(CodeNegotiationFailed, "negotiate playback pipeline", err)
	}
	return geometry, nil
}

func Build(ctx context.Context, inputs InputSet, output io.Writer, spec Spec, observation pipeline.ObservationMode) (*pipeline.Pipeline, error) {
	geometry, err := Negotiate(ctx, inputs, output, spec)
	if err != nil {
		return nil, err
	}
	built, err := godec.NewBuilder().Build(geometry, pipeline.WithObservation(observation))
	if err != nil {
		_ = geometry.Close()
		return nil, wrapError(CodeBuildFailed, "build pipeline", err)
	}
	return built, nil
}

func BuildPlayback(ctx context.Context, inputs InputSet, spec PlaybackSpec, sink PlaybackSink, observation pipeline.ObservationMode) (*pipeline.Pipeline, error) {
	geometry, err := NegotiatePlayback(ctx, inputs, spec, sink)
	if err != nil {
		return nil, err
	}
	built, err := godec.NewBuilder().Build(geometry, pipeline.WithObservation(observation))
	if err != nil {
		_ = geometry.Close()
		return nil, wrapError(CodeBuildFailed, "build playback pipeline", err)
	}
	return built, nil
}

func resolveAuxiliaryInputs(inputs InputSet, configured map[string]resolvedAuxInput) (map[string]routing.AuxInputSpec, error) {
	aux := make(map[string]routing.AuxInputSpec, len(inputs.Aux))
	for name, source := range inputs.Aux {
		if source == nil {
			return nil, invalidSpec(fmt.Sprintf("auxiliary input %q is nil", name))
		}
		value := configured[name]
		aux[name] = routing.AuxInputSpec{Source: source, DemuxManifest: value.Demuxer, DemuxConfig: value.DemuxConfig, DecoderManifest: value.Decoder, DecodeConfig: value.DecodeConfig}
	}
	for name := range configured {
		if _, ok := inputs.Aux[name]; !ok {
			return nil, invalidSpec(fmt.Sprintf("auxiliary input %q source is required", name))
		}
	}
	return aux, nil
}
