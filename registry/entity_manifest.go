package registry

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

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
	key                  PluginKey
	Name                 string
	Description          string
	ConfigurationFactory ConfigurationFactory
}

func (m BaseManifest) ID() PluginKey { return m.key }

func (m BaseManifest) RegistryName() string { return m.Name }

func (m BaseManifest) NewConfiguration() (Configuration, error) {
	if m.ConfigurationFactory == nil {
		return nil, fmt.Errorf("manifest %q has no configuration factory", m.Name)
	}
	config := m.ConfigurationFactory()
	configType, err := configurationType(config)
	if err != nil {
		return nil, err
	}
	if m.key.configType != nil && configType != m.key.configType {
		return nil, fmt.Errorf("manifest %q configuration factory returned %s, want %s", m.Name, configType, m.key.configType)
	}
	return config, nil
}

type TransformManifest struct {
	BaseManifest
	InputRequirements InputRequirementsFunc
	Resources         ResourceRequest
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
	Extensions   []string
	Codecs       []media.CodecID
	DefaultCodec media.CodecID
	Factory      MuxerFactory
}

type DemuxerManifest struct {
	BaseManifest
	Probe   manifest.Prober
	Factory DemuxerFactory
}

type EncoderManifest struct {
	TransformManifest
	Codecs  []media.CodecID
	Factory EncoderFactory
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
	return manifest.MatchesAny(requirements, stream), nil
}

func (m BaseManifest) validate() error {
	if m.Name == "" {
		return fmt.Errorf("manifest name must not be empty")
	}
	if !isManifestName(m.Name) {
		return fmt.Errorf("manifest name %q must be lower kebab-case", m.Name)
	}
	return nil
}

func isManifestName(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	previousDash := false
	for _, char := range name {
		isLower := char >= 'a' && char <= 'z'
		isDigit := char >= '0' && char <= '9'
		if char == '-' {
			if previousDash {
				return false
			}
			previousDash = true
			continue
		}
		if !isLower && !isDigit {
			return false
		}
		previousDash = false
	}
	return !previousDash
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
	if len(m.Extensions) == 0 {
		return fmt.Errorf("muxer manifest %q has no extensions", m.Name)
	}
	for _, extension := range m.Extensions {
		if !strings.HasPrefix(extension, ".") || extension != strings.ToLower(extension) || len(extension) == 1 {
			return fmt.Errorf("muxer manifest %q has invalid extension %q", m.Name, extension)
		}
	}
	if hasDuplicates(m.Extensions) {
		return fmt.Errorf("muxer manifest %q has duplicate extensions", m.Name)
	}
	if err := validateCodecs(m.Name, m.Codecs); err != nil {
		return err
	}
	if !slices.Contains(m.Codecs, m.DefaultCodec) {
		return fmt.Errorf("muxer manifest %q default codec %q is not supported", m.Name, m.DefaultCodec)
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
	if m.Factory == nil {
		return fmt.Errorf("encoder manifest %q has no factory", m.Name)
	}
	return validateCodecs(m.Name, m.Codecs)
}

func (m EncoderManifest) Supports(codec media.CodecID) bool {
	return slices.Contains(m.Codecs, codec)
}

func (m MuxerManifest) Supports(codec media.CodecID) bool {
	return slices.Contains(m.Codecs, codec)
}

func validateCodecs(name string, codecs []media.CodecID) error {
	if len(codecs) == 0 {
		return fmt.Errorf("manifest %q has no codecs", name)
	}
	for _, codec := range codecs {
		if codec == "" {
			return fmt.Errorf("manifest %q has an empty codec", name)
		}
	}
	if hasDuplicates(codecs) {
		return fmt.Errorf("manifest %q has duplicate codecs", name)
	}
	return nil
}

func hasDuplicates[T comparable](values []T) bool {
	seen := make(map[T]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
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
