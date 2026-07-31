package conversion

import (
	"fmt"

	godec "github.com/godexture/godec/core"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/core/routing"
	setting "github.com/godexture/godec/sdk/config"
)

func Resolve(spec Spec) (Resolved, error) {
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

	input, err := resolveInput(playbackSpec(spec))
	if err != nil {
		return Resolved{}, err
	}

	return Resolved{
		Demuxer: input.Demuxer, DemuxConfig: input.DemuxConfig,
		Decoder: input.Decoder, DecodeConfig: input.DecodeConfig,
		Filters: input.Filters, AuxInputs: input.AuxInputs, Sink: input.Sink,
		Encoder: encoder, EncodeConfig: encodeConfig,
		Muxer: muxer, MuxConfig: muxConfig, Codec: codec,
		Resources: input.Resources,
	}, nil
}

type resolvedInput struct {
	Demuxer      registry.DemuxerManifest
	DemuxConfig  registry.Configuration
	Decoder      registry.DecoderManifest
	DecodeConfig registry.Configuration
	Filters      []routing.FilterSpec
	AuxInputs    map[string]resolvedAuxInput
	Sink         *routing.PortRef
	Resources    registry.ResourceBudget
}

func playbackSpec(spec Spec) PlaybackSpec {
	return PlaybackSpec{
		Demuxer: spec.Demuxer, Decoder: spec.Decoder, Filters: spec.Filters,
		AuxInputs: spec.AuxInputs, Sink: spec.Sink, Parallelism: spec.Parallelism,
	}
}

func resolveInput(spec PlaybackSpec) (resolvedInput, error) {
	if spec.Parallelism < 0 {
		return resolvedInput{}, invalidSpec("parallelism must not be negative")
	}
	demuxer, demuxConfig, err := resolveOptional("demuxer", spec.Demuxer, godec.DefaultDemuxerRegistry)
	if err != nil {
		return resolvedInput{}, err
	}
	decoder, decodeConfig, err := resolveOptional("decoder", spec.Decoder, godec.DefaultDecoderRegistry)
	if err != nil {
		return resolvedInput{}, err
	}
	filters, err := resolveFilters(spec.Filters)
	if err != nil {
		return resolvedInput{}, err
	}
	auxInputs := make(map[string]resolvedAuxInput, len(spec.AuxInputs))
	for name, aux := range spec.AuxInputs {
		if name == "" {
			return resolvedInput{}, invalidSpec("auxiliary input name is required")
		}
		auxDemuxer, auxDemuxConfig, err := resolveOptional("auxiliary demuxer", aux.Demuxer, godec.DefaultDemuxerRegistry)
		if err != nil {
			return resolvedInput{}, err
		}
		auxDecoder, auxDecodeConfig, err := resolveOptional("auxiliary decoder", aux.Decoder, godec.DefaultDecoderRegistry)
		if err != nil {
			return resolvedInput{}, err
		}
		auxInputs[name] = resolvedAuxInput{Demuxer: auxDemuxer, DemuxConfig: auxDemuxConfig, Decoder: auxDecoder, DecodeConfig: auxDecodeConfig}
	}
	var sink *routing.PortRef
	if spec.Sink != nil {
		sink = &routing.PortRef{Alias: spec.Sink.Alias, Port: spec.Sink.Port}
	}
	return resolvedInput{
		Demuxer: demuxer, DemuxConfig: demuxConfig, Decoder: decoder, DecodeConfig: decodeConfig,
		Filters: filters, AuxInputs: auxInputs, Sink: sink,
		Resources: registry.ResourceBudget{Parallelism: spec.Parallelism},
	}, nil
}

func resolveFilters(filters []FilterSpec) ([]routing.FilterSpec, error) {
	resolved := make([]routing.FilterSpec, 0, len(filters))
	for i, filterSpec := range filters {
		if filterSpec.Name == "" {
			return nil, invalidSpec(fmt.Sprintf("filter %d name is required", i))
		}
		filter, lookupErr := resolveFilterManifest(filterSpec)
		if lookupErr != nil {
			return nil, lookupErr
		}
		config, configErr := configure("filter", filter, filterSpec.Values)
		if configErr != nil {
			return nil, configErr
		}
		var inputs map[string]routing.PortRef
		if filterSpec.Inputs != nil {
			inputs = make(map[string]routing.PortRef, len(filterSpec.Inputs))
			for port, ref := range filterSpec.Inputs {
				inputs[port] = routing.PortRef{Alias: ref.Alias, Port: ref.Port}
			}
		}
		resolved = append(resolved, routing.FilterSpec{Alias: filterSpec.Alias, Config: config, Inputs: inputs, Manifest: filter})
	}
	return resolved, nil
}

// resolveFilterManifest looks the filter up by name, trying the ordinary
// (static) registry first and the parameterized registry second. A
// parameterized filter's Parameters are decoded and used to build its
// concrete FilterManifest for this one invocation before the filter's
// regular per-instance Values are ever touched — the manifest that
// produces (in particular its ConfigurationFactory) may depend on those
// Parameters, e.g. a mixer's input/output port count.
func resolveFilterManifest(filterSpec FilterSpec) (registry.FilterManifest, error) {
	manifest, err := godec.DefaultFilterRegistry.Lookup(filterSpec.Name)
	if err == nil {
		if filterSpec.Parameters != nil {
			return registry.FilterManifest{}, invalidSpec(fmt.Sprintf("filter %q does not accept parameters", filterSpec.Name))
		}
		return manifest, nil
	}

	parameterized, paramErr := godec.DefaultParameterizedFilterRegistry.Lookup(filterSpec.Name)
	if paramErr != nil {
		return registry.FilterManifest{}, wrapError(CodeInvalidSpec, fmt.Sprintf("filter %q", filterSpec.Name), err)
	}
	parameters, configErr := configure("filter parameters", parameterized, filterSpec.Parameters)
	if configErr != nil {
		return registry.FilterManifest{}, configErr
	}
	manifest, err = parameterized.NewManifest(parameters)
	if err != nil {
		return registry.FilterManifest{}, wrapError(CodeInvalidSpec, fmt.Sprintf("filter %q", filterSpec.Name), err)
	}
	return manifest, nil
}

func configure(role string, value registry.Manifest, values map[string]string) (registry.Configuration, error) {
	config, _, err := setting.Resolve(value, values, setting.Strict)
	if err != nil {
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
