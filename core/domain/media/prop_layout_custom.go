package media

import "math/bits"

func NewCustomLayout(layout ...ChannelPosition) ChannelLayout {
	mapping := make([]byte, len(layout))
	for i, channel := range layout {
		if channel == 0 || channel&(channel-1) != 0 {
			panic("custom layout channel must be exactly one channel position")
		}
		mapping[i] = byte(bits.TrailingZeros64(uint64(channel)))
	}

	return ChannelLayout{
		order:        OrderCustom,
		channelCount: len(layout),
		custom:       string(mapping),
	}
}

func (l ChannelLayout) CustomChannels() []ChannelPosition {
	if l.order != OrderCustom {
		return nil
	}
	channels := make([]ChannelPosition, len(l.custom))
	for i, index := range []byte(l.custom) {
		channels[i] = 1 << index
	}
	return channels
}
