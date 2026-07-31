package media

import (
	"fmt"
	"math/bits"
	"slices"
	"strconv"
)

type ChannelCategory uint8

const (
	OrderUnspecified ChannelCategory = iota
	OrderNative
	OrderCustom
	OrderAmbisonic
)

type ChannelLayout struct {
	order        ChannelCategory
	channelCount int

	value  uint64
	custom string
}

func (l ChannelLayout) Mask() ChannelPosition {
	if l.order == OrderNative {
		return ChannelPosition(l.value)
	}
	return 0
}

func (l ChannelLayout) IsUnspecified() bool {
	return l.order == OrderUnspecified
}

func (l ChannelLayout) IsSpatial() bool {
	return l.order == OrderNative || l.order == OrderCustom
}

func (l ChannelLayout) IsAmbisonic() bool {
	return l.order == OrderAmbisonic
}

func (l ChannelLayout) Order() ChannelCategory { return l.order }

func (l ChannelLayout) AmbisonicOrder() uint8 {
	if l.order != OrderAmbisonic {
		return 0
	}
	return uint8(l.value)
}

func (l ChannelLayout) ChannelCount() int { return l.channelCount }

func (l ChannelLayout) Contains(c ChannelPosition) bool {
	switch l.order {

	case OrderNative:
		return (l.Mask() & c) != 0

	case OrderCustom:
		return slices.Contains(l.CustomChannels(), c)

	default:
		return false
	}
}

func (l ChannelLayout) Enumerate() []ChannelPosition {
	switch l.order {
	case OrderNative:
		var channels []ChannelPosition
		for i := 0; i < 64; i++ {
			if l.Mask()&(1<<i) != 0 {
				channels = append(channels, 1<<i)
			}
		}
		return channels

	case OrderCustom:
		return append([]ChannelPosition{}, l.CustomChannels()...)

	default:
		return nil
	}
}

func (l ChannelLayout) Index(c ChannelPosition) int {
	switch l.order {

	case OrderNative:
		if (l.Mask() & c) == 0 {
			return -1
		}

		return bits.OnesCount64(uint64(l.Mask() & (c - 1)))

	case OrderCustom:
		return slices.Index(l.CustomChannels(), c)

	default:
		return -1

	}
}

func (l ChannelLayout) Validate() error {
	if l.channelCount < 0 {
		return fmt.Errorf("negative channel count: %d", l.channelCount)
	}
	switch l.order {
	case OrderUnspecified:
		if l.value != 0 || l.custom != "" {
			return fmt.Errorf("unspecified layout has channel mapping")
		}
	case OrderNative:
		if bits.OnesCount64(l.value) != l.channelCount || l.custom != "" {
			return fmt.Errorf("native layout channel count does not match mask")
		}
	case OrderCustom:
		if len(l.custom) != l.channelCount || l.value != 0 {
			return fmt.Errorf("custom layout channel mapping is invalid")
		}
	case OrderAmbisonic:
		order := int(l.value)
		if order < 0 || (order+1)*(order+1) != l.channelCount || l.custom != "" {
			return fmt.Errorf("ambisonic layout order does not match channel count")
		}
	default:
		return fmt.Errorf("unknown channel layout order: %d", l.order)
	}
	return nil
}

func (l ChannelLayout) String() string {
	switch l.order {
	case OrderUnspecified:
		return "unspecified(" + strconv.Itoa(l.channelCount) + "ch)"
	case OrderNative:
		return "native(" + joinChannelPositions(l.Enumerate()) + ")"
	case OrderCustom:
		return "custom(" + joinChannelPositions(l.CustomChannels()) + ")"
	case OrderAmbisonic:
		return "ambisonic(order=" + strconv.Itoa(int(l.AmbisonicOrder())) + "," + strconv.Itoa(l.channelCount) + "ch)"
	default:
		return "invalid(" + strconv.Itoa(l.channelCount) + "ch)"
	}
}
