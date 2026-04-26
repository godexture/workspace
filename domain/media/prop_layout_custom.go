package media

import (
	"math/bits"

	"github.com/godexture/core/internal/xsync"
	"github.com/godexture/sdk/hash"
)

type CustomLayoutID uint64

var (
	customRegistry = xsync.NewMap[CustomLayoutID, []ChannelPosition]()
)

func calculateCustomID(mapping []ChannelPosition) CustomLayoutID {
	length := len(mapping)
	if length == 0 {
		return 0
	}

	pos := make([]byte, length)
	for i, c := range mapping {
		pos[i] = byte(bits.TrailingZeros64(uint64(c)))
	}

	if length <= 10 {
		id := CustomLayoutID(length) << 60

		for i, idx := range pos {
			id |= CustomLayoutID(idx) << (i * 6)
		}

		return id
	}

	return CustomLayoutID(hash.FNV(pos))
}

func NewCustomLayout(layout ...ChannelPosition) ChannelLayout {
	id := calculateCustomID(layout)

	customRegistry.Store(id, layout)

	return ChannelLayout{
		order:        OrderCustom,
		channelCount: len(layout),
		value:        uint64(id),
	}
}

func (l ChannelLayout) CustomChannels() []ChannelPosition {
	if l.order == OrderCustom {
		if channels, exists := customRegistry.Load(l.CustomID()); exists {
			return channels
		}
	}

	return nil
}
