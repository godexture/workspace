package conversion

import (
	"context"
	"fmt"
	"io"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/core/registry"
	"github.com/godexture/core/routing"
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
	aux := make(map[string]routing.AuxInputSpec, len(inputs.Aux))
	for name, source := range inputs.Aux {
		if source == nil {
			return nil, invalidSpec(fmt.Sprintf("auxiliary input %q is nil", name))
		}
		configured := resolved.AuxInputs[name]
		aux[name] = routing.AuxInputSpec{Source: source, DemuxManifest: configured.Demuxer, DemuxConfig: configured.DemuxConfig, DecoderManifest: configured.Decoder, DecodeConfig: configured.DecodeConfig}
	}
	for name := range resolved.AuxInputs {
		if _, ok := inputs.Aux[name]; !ok {
			return nil, invalidSpec(fmt.Sprintf("auxiliary input %q source is required", name))
		}
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
