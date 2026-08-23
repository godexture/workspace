package codec

import (
	"fmt"

	"github.com/godexture/godec/media/property"
)

// Block describes a codec whose bitstream is a run of fixed-size blocks. A
// packet of such a stream carries whole blocks and never part of one.
//
// Bytes is what a container states, because the size of a block is how it
// finds the next one. Samples is what the codec states, because how many
// samples a block yields follows from the coding rather than from the
// container, and a container that never read the codec extension does not
// know it. Either may stand alone.
type Block struct {
	Bytes   int
	Samples int
}

func (b Block) Valid() bool { return b.Bytes > 0 && b.Samples >= 0 }

// Stated reports whether the stream is block-coded at all.
func (b Block) Stated() bool { return b.Bytes > 0 }

type blockKeyID struct{}

var blockProperty = property.Define[blockKeyID](func(value Block) ([]byte, error) {
	if !value.Valid() {
		return nil, fmt.Errorf("codec block geometry is invalid")
	}
	return fmt.Appendf(nil, "codec-block:%d/%d", value.Bytes, value.Samples), nil
})

func WithBlock(properties property.Set, value Block) (property.Set, error) {
	return blockProperty.Set(properties, value)
}

func BlockOf(properties property.Set) (Block, bool) {
	value, ok := blockProperty.Get(properties)
	return value, ok && value.Stated()
}

// WithoutBlock drops the block geometry, which is what a decoded stream does:
// its samples are no longer grouped the way the coded ones were.
func WithoutBlock(properties property.Set) property.Set {
	return properties.Delete(blockProperty.ID())
}
