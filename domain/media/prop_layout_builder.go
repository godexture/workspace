package media

import "math/bits"

func NewUnspecified(channels int) ChannelLayout {
	return ChannelLayout{
		order:        OrderUnspecified,
		channelCount: channels,
	}
}

func NewNativeLayout(mask ChannelPosition) ChannelLayout {
	return ChannelLayout{
		order:        OrderNative,
		channelCount: bits.OnesCount64(uint64(mask)),
		value:        uint64(mask),
	}
}

func NewAmbisonicLayout(order uint8) ChannelLayout {
	channelCount := (int(order) + 1) * (int(order) + 1)
	return ChannelLayout{
		order:        OrderAmbisonic,
		channelCount: channelCount,
		value:        uint64(order),
	}
}
