package registry

import (
	"io"
	"reflect"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

type Configuration interface{}

// ConfigurationFactory produces a plugin's Configuration. ConfigurationType
// reports the concrete configuration type without constructing (and
// therefore without validating) a value, so the registry can derive plugin
// identity even for configurations with no semantically valid default.
type ConfigurationFactory interface {
	ConfigurationType() reflect.Type
	New() (Configuration, error)

	// Default returns the factory's default field values without applying
	// options or validating the result. It is intended as a starting point
	// for further mutation (e.g. decoding CLI/spec values onto it) that is
	// validated once, after all values are applied — not before.
	Default() Configuration
}

type optionConfigurationFactory[T any] struct {
	construct func() (T, error)
}

func (f optionConfigurationFactory[T]) ConfigurationType() reflect.Type {
	return reflect.TypeFor[T]()
}

func (f optionConfigurationFactory[T]) New() (Configuration, error) {
	configuration, err := f.construct()
	if err != nil {
		return nil, err
	}
	return &configuration, nil
}

func (f optionConfigurationFactory[T]) Default() Configuration {
	configuration, _ := f.construct()
	return &configuration
}

func NewConfigurationFactory[T any, Option any](newConfiguration func(...Option) (T, error)) ConfigurationFactory {
	return optionConfigurationFactory[T]{construct: func() (T, error) { return newConfiguration() }}
}

type staticConfigurationFactory struct {
	config Configuration
}

func (f staticConfigurationFactory) ConfigurationType() reflect.Type {
	configType, err := configurationType(f.config)
	if err != nil {
		return nil
	}
	return configType
}

func (f staticConfigurationFactory) New() (Configuration, error) {
	return f.clone(), nil
}

func (f staticConfigurationFactory) Default() Configuration {
	return f.clone()
}

// clone returns a fresh copy of f.config, so repeated calls to New or
// Default never hand out a shared, mutable instance — matching the
// option-pattern factory's guarantee.
func (f staticConfigurationFactory) clone() Configuration {
	value := reflect.ValueOf(f.config)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() {
		return f.config
	}
	clone := reflect.New(value.Type().Elem())
	clone.Elem().Set(value.Elem())
	return clone.Interface()
}

// StaticConfigurationFactory wraps an already-constructed Configuration
// value. Intended for tests and other call sites where the value is known
// upfront rather than produced by an option-pattern constructor. Each call
// to New or Default returns a fresh copy, matching the option-pattern
// factory's guarantee that callers never share a mutable instance.
func StaticConfigurationFactory(config Configuration) ConfigurationFactory {
	return staticConfigurationFactory{config: config}
}

type MuxerFactory func(io.Writer, Configuration) (node.Muxer, error)
type DemuxerFactory func(io.Reader, Configuration) (node.Demuxer, error)

type TransformFactoryOptions struct {
	Config Configuration
}

// Preparer is an optional node capability for resource-dependent setup. It is
// called after routing and linking, before the pipeline starts processing.
type Preparer interface {
	Prepare(ResourceGrant) error
}

type EncoderFactory func(media.StreamInfo, media.CodecID, TransformFactoryOptions) (node.Encoder, media.StreamInfo, error)
type DecoderFactory func(media.StreamInfo, TransformFactoryOptions) (node.Decoder, media.StreamInfo, error)

// FilterFactory builds a filter node from the resolved stream on each of its
// input ports, and reports the resolved stream on each of its output ports.
// Single-port filters normally use SingleFactory instead of implementing
// this directly.
type FilterFactory func(media.StreamSet, TransformFactoryOptions) (node.Filter, media.StreamSet, error)

// SingleFactory adapts a conventional single "in"->"out" filter factory to
// the general per-port FilterFactory signature.
func SingleFactory(factory func(media.StreamInfo, TransformFactoryOptions) (node.Filter, media.StreamInfo, error)) FilterFactory {
	return func(inputs media.StreamSet, options TransformFactoryOptions) (node.Filter, media.StreamSet, error) {
		filter, output, err := factory(inputs["in"], options)
		if err != nil {
			return nil, nil, err
		}
		return filter, media.StreamSet{"out": output}, nil
	}
}
