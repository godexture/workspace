package registry

import (
	"fmt"
	"reflect"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
)

type Defaulter interface {
	ApplyDefaults()
}

type Validator interface {
	Validate() error
}

type BaseManifest struct {
	key         PluginKey
	Name        string
	Description string
}

func (m BaseManifest) ID() PluginKey { return m.key }

type TransformManifest struct {
	BaseManifest
	InputRequirements InputRequirementsFunc
	Resources         ResourceRequest
	// TransformFunc resolves the output profile for this transform. target is
	// the desired codec (the input codec for decoders) and cfg is the node
	// configuration that will be used to construct the transform.
	TransformFunc func(in media.StreamInfo, target media.CodecID, cfg Configuration) (media.Profile, error)
}

type InputRequirementsFunc func(target media.CodecID, config Configuration) ([]manifest.Capability, error)

func StaticRequirements(capabilities ...manifest.Capability) InputRequirementsFunc {
	return func(media.CodecID, Configuration) ([]manifest.Capability, error) {
		return capabilities, nil
	}
}

type ConversionCost struct {
	QualityLoss uint32
	Work        uint32
}

type ConversionCandidate struct {
	Config Configuration
	Cost   ConversionCost
}

type BridgeFunc func(
	current media.StreamInfo,
	required []manifest.Capability,
) ([]ConversionCandidate, error)

type MuxerManifest struct {
	BaseManifest
	Factory MuxerFactory
}

type DemuxerManifest struct {
	BaseManifest
	Probe   manifest.Prober
	Factory DemuxerFactory
}

type EncoderManifest struct {
	TransformManifest
	Supports func(codec media.CodecID) bool
	Factory  EncoderFactory
}

type DecoderManifest struct {
	TransformManifest
	Factory DecoderFactory
}

type FilterManifest struct {
	TransformManifest
	Bridge  BridgeFunc
	Factory FilterFactory
}

func (m TransformManifest) Transform(stream media.StreamInfo, target media.CodecID, cfg Configuration) (media.Profile, error) {
	return m.TransformFunc(stream, target, cfg)
}

func (m TransformManifest) TransformStream(stream media.StreamInfo, target media.CodecID, cfg Configuration) (media.StreamInfo, error) {
	if m.TransformFunc == nil {
		return stream, nil
	}
	profile, err := m.Transform(stream, target, cfg)
	if err != nil {
		return media.StreamInfo{}, err
	}
	stream.Type = profile.Type
	stream.MediaAttributes = profile.MediaAttributes
	return stream, nil
}

func (m TransformManifest) Requirements(target media.CodecID, config Configuration) ([]manifest.Capability, error) {
	if m.InputRequirements == nil {
		return nil, fmt.Errorf("transform manifest %q has no input requirements", m.Name)
	}
	requirements, err := m.InputRequirements(target, config)
	if err != nil {
		return nil, err
	}
	if len(requirements) == 0 {
		return nil, fmt.Errorf("transform manifest %q has no input requirements", m.Name)
	}
	for i, capability := range requirements {
		if isNilCapability(capability) {
			return nil, fmt.Errorf("transform manifest %q input requirement %d is nil", m.Name, i)
		}
	}
	return requirements, nil
}

func (m TransformManifest) Accept(stream media.StreamInfo, target media.CodecID, config Configuration) (bool, error) {
	requirements, err := m.Requirements(target, config)
	if err != nil {
		return false, err
	}
	for _, c := range requirements {
		if c.Match(stream) {
			return true, nil
		}
	}
	return false, nil
}

func (m BaseManifest) validate() error {
	if m.Name == "" {
		return fmt.Errorf("manifest name must not be empty")
	}
	return nil
}

func (m TransformManifest) validate() error {
	if err := m.BaseManifest.validate(); err != nil {
		return err
	}
	if m.InputRequirements == nil {
		return fmt.Errorf("transform manifest %q must declare input requirements", m.Name)
	}
	return nil
}

func isNilCapability(capability manifest.Capability) bool {
	if capability == nil {
		return true
	}
	value := reflect.ValueOf(capability)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

func (m MuxerManifest) Validate() error {
	if err := m.BaseManifest.validate(); err != nil {
		return err
	}
	if m.Factory == nil {
		return fmt.Errorf("muxer manifest %q has no factory", m.Name)
	}
	return nil
}

func (m DemuxerManifest) Validate() error {
	if err := m.BaseManifest.validate(); err != nil {
		return err
	}
	if m.Probe == nil {
		return fmt.Errorf("demuxer manifest %q has no probe", m.Name)
	}
	if m.Factory == nil {
		return fmt.Errorf("demuxer manifest %q has no factory", m.Name)
	}
	return nil
}

func (m EncoderManifest) Validate() error {
	if err := m.TransformManifest.validate(); err != nil {
		return err
	}
	if m.Supports == nil {
		return fmt.Errorf("encoder manifest %q has no codec matcher", m.Name)
	}
	if m.Factory == nil {
		return fmt.Errorf("encoder manifest %q has no factory", m.Name)
	}
	return nil
}

func (m DecoderManifest) Validate() error {
	if err := m.TransformManifest.validate(); err != nil {
		return err
	}
	if m.Factory == nil {
		return fmt.Errorf("decoder manifest %q has no factory", m.Name)
	}
	return nil
}

func (m FilterManifest) Validate() error {
	if err := m.TransformManifest.validate(); err != nil {
		return err
	}
	if m.Factory == nil {
		return fmt.Errorf("filter manifest %q has no factory", m.Name)
	}
	return nil
}
