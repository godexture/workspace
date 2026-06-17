package registry

import (
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
	id          reflect.Type
	Name        string
	Description string
}

func (m BaseManifest) ID() reflect.Type { return m.id }

type TransformManifest struct {
	BaseManifest
	Capabilities  []manifest.Capability
	TransformFunc func(p media.StreamInfo) media.Profile
}

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
	Factory FilterFactory
}

func (m TransformManifest) Transform(stream media.StreamInfo) media.Profile {
	return m.TransformFunc(stream)
}

func (m TransformManifest) Accept(stream media.StreamInfo) bool {
	for _, c := range m.Capabilities {
		if c.Match(stream) {
			return true
		}
	}
	return false
}
