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
	Capabilities []manifest.Capability
	// TransformFunc resolves the output profile for this transform. target is
	// the desired codec (the input codec for decoders) and cfg is the node
	// configuration that will be used to construct the transform.
	TransformFunc func(in media.StreamInfo, target media.CodecID, cfg Configuration) (media.Profile, error)
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

func (m TransformManifest) Transform(stream media.StreamInfo, target media.CodecID, cfg Configuration) (media.Profile, error) {
	return m.TransformFunc(stream, target, cfg)
}

func (m TransformManifest) Accept(stream media.StreamInfo) bool {
	for _, c := range m.Capabilities {
		if c.Match(stream) {
			return true
		}
	}
	return false
}
