package conversion

import (
	"context"
	"fmt"
	"io"
	"maps"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/core/registry"
	"github.com/godexture/core/routing"
	"github.com/godexture/sdk/cliflag"
)

type PluginSpec struct {
	Name   string            `json:"name"`
	Values map[string]string `json:"values,omitempty"`
}

type FilterSpec struct {
	PluginSpec
	Alias  string            `json:"alias,omitempty"`
	Inputs map[string]string `json:"inputs,omitempty"`
}

type AuxInputSpec struct {
	Demuxer *PluginSpec  `json:"demuxer,omitempty"`
	Decoder *PluginSpec  `json:"decoder,omitempty"`
	Filters []FilterSpec `json:"filters,omitempty"`
}

type InputSet struct {
	Main io.ReadSeeker
	Aux  map[string]io.ReadSeeker
}

type Spec struct {
	Demuxer     *PluginSpec             `json:"demuxer,omitempty"`
	Decoder     *PluginSpec             `json:"decoder,omitempty"`
	Filters     []FilterSpec            `json:"filters,omitempty"`
	AuxInputs   map[string]AuxInputSpec `json:"auxInputs,omitempty"`
	Codec       string                  `json:"codec,omitempty"`
	Encoder     *PluginSpec             `json:"encoder,omitempty"`
	Muxer       PluginSpec              `json:"muxer"`
	Parallelism int                     `json:"parallelism,omitempty"`
}

type Resolved struct {
	Demuxer      registry.DemuxerManifest
	DemuxConfig  registry.Configuration
	Decoder      registry.DecoderManifest
	DecodeConfig registry.Configuration
	Filters      []routing.FilterSpec
	AuxInputs    map[string]resolvedAuxInput
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
	Filters      []routing.FilterSpec
}

func Resolve(spec Spec) (Resolved, error) {
	if spec.Parallelism < 0 {
		return Resolved{}, invalidSpec("parallelism must not be negative")
	}
	if spec.Muxer.Name == "" {
		return Resolved{}, invalidSpec("muxer name is required")
	}

	muxer, err := godec.DefaultMuxerRegistry.Lookup(spec.Muxer.Name)
	if err != nil {
		return Resolved{}, wrapError(CodeInvalidSpec, fmt.Sprintf("muxer %q", spec.Muxer.Name), err)
	}
	muxConfig, err := configure("muxer", muxer, spec.Muxer.Values)
	if err != nil {
		return Resolved{}, err
	}

	codec := media.CodecID(spec.Codec)
	if codec == "" {
		codec = muxer.DefaultCodec
	}
	if !muxer.Supports(codec) {
		return Resolved{}, newError(CodeUnsupportedCodec, fmt.Sprintf("muxer %q does not support codec %q", muxer.Name, codec))
	}

	var encoder registry.EncoderManifest
	if spec.Encoder != nil && spec.Encoder.Name != "" {
		encoder, err = godec.DefaultEncoderRegistry.Lookup(spec.Encoder.Name)
	} else {
		encoder, err = godec.NewResolver().NewEncoderResolver(godec.DefaultEncoderRegistry).ResolveEncoder(codec)
	}
	if err != nil {
		return Resolved{}, wrapError(CodeUnsupportedCodec, fmt.Sprintf("encoder for codec %q", codec), err)
	}
	if !encoder.Supports(codec) {
		return Resolved{}, newError(CodeUnsupportedCodec, fmt.Sprintf("encoder %q does not support codec %q", encoder.Name, codec))
	}
	var encoderValues map[string]string
	if spec.Encoder != nil {
		encoderValues = spec.Encoder.Values
	}
	encodeConfig, err := configure("encoder", encoder, encoderValues)
	if err != nil {
		return Resolved{}, err
	}

	demuxer, demuxConfig, err := resolveOptional("demuxer", spec.Demuxer, godec.DefaultDemuxerRegistry)
	if err != nil {
		return Resolved{}, err
	}
	decoder, decodeConfig, err := resolveOptional("decoder", spec.Decoder, godec.DefaultDecoderRegistry)
	if err != nil {
		return Resolved{}, err
	}

	filters, err := resolveFilters(spec.Filters)
	if err != nil {
		return Resolved{}, err
	}
	auxInputs := make(map[string]resolvedAuxInput, len(spec.AuxInputs))
	for name, aux := range spec.AuxInputs {
		if name == "" {
			return Resolved{}, invalidSpec("auxiliary input name is required")
		}
		demuxer, demuxConfig, err := resolveOptional("auxiliary demuxer", aux.Demuxer, godec.DefaultDemuxerRegistry)
		if err != nil {
			return Resolved{}, err
		}
		decoder, decodeConfig, err := resolveOptional("auxiliary decoder", aux.Decoder, godec.DefaultDecoderRegistry)
		if err != nil {
			return Resolved{}, err
		}
		auxFilters, err := resolveFilters(aux.Filters)
		if err != nil {
			return Resolved{}, err
		}
		auxInputs[name] = resolvedAuxInput{Demuxer: demuxer, DemuxConfig: demuxConfig, Decoder: decoder, DecodeConfig: decodeConfig, Filters: auxFilters}
	}

	return Resolved{
		Demuxer: demuxer, DemuxConfig: demuxConfig,
		Decoder: decoder, DecodeConfig: decodeConfig,
		Filters: filters, AuxInputs: auxInputs,
		Encoder: encoder, EncodeConfig: encodeConfig,
		Muxer: muxer, MuxConfig: muxConfig, Codec: codec,
		Resources: registry.ResourceBudget{Parallelism: spec.Parallelism},
	}, nil
}

func resolveFilters(filters []FilterSpec) ([]routing.FilterSpec, error) {
	resolved := make([]routing.FilterSpec, 0, len(filters))
	for i, filterSpec := range filters {
		if filterSpec.Name == "" {
			return nil, invalidSpec(fmt.Sprintf("filter %d name is required", i))
		}
		filter, lookupErr := godec.DefaultFilterRegistry.Lookup(filterSpec.Name)
		if lookupErr != nil {
			return nil, wrapError(CodeInvalidSpec, fmt.Sprintf("filter %q", filterSpec.Name), lookupErr)
		}
		config, configErr := configure("filter", filter, filterSpec.Values)
		if configErr != nil {
			return nil, configErr
		}
		resolved = append(resolved, routing.FilterSpec{Alias: filterSpec.Alias, Config: config, Inputs: maps.Clone(filterSpec.Inputs)})
	}
	return resolved, nil
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
		aux[name] = routing.AuxInputSpec{Source: source, DemuxManifest: configured.Demuxer, DemuxConfig: configured.DemuxConfig, DecoderManifest: configured.Decoder, DecodeConfig: configured.DecodeConfig, Filters: configured.Filters}
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

func configure(role string, value registry.Manifest, values map[string]string) (registry.Configuration, error) {
	config, err := value.NewConfiguration()
	if err != nil {
		return nil, wrapError(CodeInvalidSpec, fmt.Sprintf("configure %s %q", role, value.RegistryName()), err)
	}
	if err := cliflag.DecodeStruct(config, values); err != nil {
		return nil, wrapError(CodeInvalidSpec, fmt.Sprintf("configure %s %q", role, value.RegistryName()), err)
	}
	return config, nil
}

func resolveOptional[V registry.Manifest](role string, spec *PluginSpec, values *registry.Registry[V]) (V, registry.Configuration, error) {
	var zero V
	if spec == nil || spec.Name == "" {
		return zero, nil, nil
	}
	value, err := values.Lookup(spec.Name)
	if err != nil {
		return zero, nil, wrapError(CodeInvalidSpec, fmt.Sprintf("%s %q", role, spec.Name), err)
	}
	config, err := configure(role, value, spec.Values)
	if err != nil {
		return zero, nil, err
	}
	return value, config, nil
}
