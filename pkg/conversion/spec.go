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
	"github.com/godexture/sdk/cliflag"
)

type PluginSpec struct {
	Name   string            `json:"name"`
	Values map[string]string `json:"values,omitempty"`
}

type FilterSpec struct {
	PluginSpec
}

type Spec struct {
	Demuxer     *PluginSpec  `json:"demuxer,omitempty"`
	Decoder     *PluginSpec  `json:"decoder,omitempty"`
	Filters     []FilterSpec `json:"filters,omitempty"`
	Codec       string       `json:"codec,omitempty"`
	Encoder     *PluginSpec  `json:"encoder,omitempty"`
	Muxer       PluginSpec   `json:"muxer"`
	Parallelism int          `json:"parallelism,omitempty"`
}

type Resolved struct {
	Demuxer      registry.DemuxerManifest
	DemuxConfig  registry.Configuration
	Decoder      registry.DecoderManifest
	DecodeConfig registry.Configuration
	Filters      []routing.FilterSpec
	Encoder      registry.EncoderManifest
	EncodeConfig registry.Configuration
	Muxer        registry.MuxerManifest
	MuxConfig    registry.Configuration
	Codec        media.CodecID
	Resources    registry.ResourceBudget
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

	filters := make([]routing.FilterSpec, 0, len(spec.Filters))
	for i, filterSpec := range spec.Filters {
		if filterSpec.Name == "" {
			return Resolved{}, invalidSpec(fmt.Sprintf("filter %d name is required", i))
		}
		filter, lookupErr := godec.DefaultFilterRegistry.Lookup(filterSpec.Name)
		if lookupErr != nil {
			return Resolved{}, wrapError(CodeInvalidSpec, fmt.Sprintf("filter %q", filterSpec.Name), lookupErr)
		}
		config, configErr := configure("filter", filter, filterSpec.Values)
		if configErr != nil {
			return Resolved{}, configErr
		}
		filters = append(filters, routing.FilterSpec{Config: config})
	}

	return Resolved{
		Demuxer: demuxer, DemuxConfig: demuxConfig,
		Decoder: decoder, DecodeConfig: decodeConfig,
		Filters: filters,
		Encoder: encoder, EncodeConfig: encodeConfig,
		Muxer: muxer, MuxConfig: muxConfig, Codec: codec,
		Resources: registry.ResourceBudget{Parallelism: spec.Parallelism},
	}, nil
}

func Negotiate(ctx context.Context, input io.ReadSeeker, output io.Writer, spec Spec) (*pipeline.Geometry, error) {
	resolved, err := Resolve(spec)
	if err != nil {
		return nil, err
	}
	geometry, err := godec.NewNegotiator().NegotiateConversion(ctx, routing.ConversionSpec{
		Input: input, Output: output,
		DemuxManifest: resolved.Demuxer, DemuxConfig: resolved.DemuxConfig,
		DecoderManifest: resolved.Decoder, DecodeConfig: resolved.DecodeConfig,
		Filters:         resolved.Filters,
		EncoderManifest: resolved.Encoder, TargetCodec: resolved.Codec, EncodeConfig: resolved.EncodeConfig,
		MuxManifest: resolved.Muxer, MuxConfig: resolved.MuxConfig,
		Resources: resolved.Resources,
	})
	if err != nil {
		return nil, wrapError(CodeNegotiationFailed, "negotiate pipeline", err)
	}
	return geometry, nil
}

func Build(ctx context.Context, input io.ReadSeeker, output io.Writer, spec Spec, observation pipeline.ObservationMode) (*pipeline.Pipeline, error) {
	geometry, err := Negotiate(ctx, input, output, spec)
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
