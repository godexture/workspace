package registry

import (
	"fmt"
	"slices"

	"github.com/godexture/core/domain/media"
)

type EncoderManifest struct {
	TransformManifest
	Codecs  []media.CodecID
	Factory EncoderFactory
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
