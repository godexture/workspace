package registry

import (
	"fmt"
	"slices"
	"strings"

	"github.com/godexture/core/domain/media"
)

type MuxerManifest struct {
	BaseManifest
	Extensions   []string
	Codecs       []media.CodecID
	DefaultCodec media.CodecID
	Factory      MuxerFactory
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
