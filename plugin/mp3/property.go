package mp3

import (
	"fmt"

	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/plugin"
)

type framingKeyID struct{}

// framing is the optional invariant a top-level MP3 reader learned while
// inspecting the stream. A parser can also derive the table bound at runtime
// for ordinary fixed-bitrate chunks.
type framing struct {
	version       uint8
	layer         uint8
	rate          int
	channels      int
	freeBytes     int
	maxFrameBytes int
}

func (f framing) valid() bool {
	return f.version <= 2 && f.layer >= 1 && f.layer <= 3 && f.rate > 0 &&
		(f.channels == 1 || f.channels == 2) && f.maxFrameBytes >= 4 &&
		(f.freeBytes == 0 || f.freeBytes >= 4)
}

var framingProperty = property.Define[framingKeyID](func(value framing) ([]byte, error) {
	if !value.valid() {
		return nil, fmt.Errorf("MP3 framing property is invalid")
	}
	return fmt.Appendf(nil, "mp3-framing:%d/%d/%d/%d/%d/%d", value.version, value.layer, value.rate, value.channels, value.freeBytes, value.maxFrameBytes), nil
})

func framingDeclaration() plugin.Declaration { return plugin.DeclareKey(framingProperty) }

func withFraming(properties property.Set, value framing) (property.Set, error) {
	return framingProperty.Set(properties, value)
}

func framingOf(properties property.Set) (framing, bool) {
	value, ok := framingProperty.Get(properties)
	return value, ok && value.valid()
}

func withoutFraming(properties property.Set) property.Set {
	return properties.Delete(framingProperty.ID())
}
