package media

import (
	"fmt"
	"math/bits"
	"slices"
	"strconv"
	"strings"
)

type ChannelCategory uint8
type ChannelPosition uint64

const (
	OrderUnspecified ChannelCategory = iota
	OrderNative
	OrderCustom
	OrderAmbisonic
)

const (
	FrontLeft ChannelPosition = 1 << iota
	FrontRight
	FrontCenter
	LowFrequency
	BackLeft
	BackRight
	FrontLeftOfCenter
	FrontRightOfCenter
	BackCenter
	SideLeft
	SideRight
	TopCenter
	TopFrontLeft
	TopFrontCenter
	TopFrontRight
	TopBackLeft
	TopBackCenter
	TopBackRight
)

var (
	LayoutMono1        ChannelLayout = NewNativeLayout(FrontCenter)
	LayoutDualMono2    ChannelLayout = NewCustomLayout(FrontCenter, FrontCenter)
	LayoutStereo2_0    ChannelLayout = NewNativeLayout(FrontLeft | FrontRight)
	LayoutStereo2_1    ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | LowFrequency)
	LayoutStereo3_0    ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter)
	LayoutStereo3_1    ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | LowFrequency)
	LayoutSurround3_0  ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | BackCenter)
	LayoutQuad4_0      ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | BackLeft | BackRight)
	LayoutSideQuad4_0  ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | SideLeft | SideRight)
	LayoutSurround4_0  ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | BackCenter)
	LayoutSurround4_1  ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | LowFrequency | BackCenter)
	LayoutFront5_0     ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | BackLeft | BackRight)
	LayoutFront5_1     ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | BackLeft | BackRight | LowFrequency)
	LayoutSide5_0      ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | SideLeft | SideRight | FrontCenter)
	LayoutSide5_1      ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | SideLeft | SideRight | FrontCenter | LowFrequency)
	LayoutAtmos5_1_4   ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | LowFrequency | BackLeft | BackRight | TopFrontLeft | TopFrontRight | TopBackLeft | TopBackRight)
	LayoutHexagonal6_0 ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | BackLeft | BackRight | BackCenter)
	LayoutHexagonal6_1 ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | LowFrequency | BackLeft | BackRight | BackCenter)
	LayoutFront6_0     ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | FrontLeftOfCenter | FrontRightOfCenter | BackCenter)
	LayoutSide6_0      ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | SideLeft | SideRight | FrontCenter | BackCenter)
	LayoutSide6_1      ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | SideLeft | SideRight | FrontCenter | LowFrequency | BackCenter)
	LayoutWide7_1      ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | LowFrequency | BackLeft | BackRight | FrontLeftOfCenter | FrontRightOfCenter)
	LayoutSide7_0      ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | SideLeft | SideRight | FrontCenter | BackLeft | BackRight)
	LayoutSide7_1      ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | SideLeft | SideRight | FrontCenter | BackLeft | BackRight | LowFrequency)
	LayoutSurround7_1  ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | LowFrequency | BackLeft | BackRight | SideLeft | SideRight)
	LayoutAtmos7_1_4   ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | LowFrequency | BackLeft | BackRight | SideLeft | SideRight | TopFrontLeft | TopFrontRight | TopBackLeft | TopBackRight)
	LayoutOctagonal8_0 ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | BackLeft | BackRight | BackCenter | SideLeft | SideRight)
	LayoutSurround9_0  ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | BackLeft | BackRight | SideLeft | SideRight | FrontLeftOfCenter | FrontRightOfCenter)
	LayoutSurround9_1  ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | LowFrequency | BackLeft | BackRight | SideLeft | SideRight | FrontLeftOfCenter | FrontRightOfCenter)
	LayoutSurround11_0 ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | BackLeft | BackRight | SideLeft | SideRight | FrontLeftOfCenter | FrontRightOfCenter | BackCenter | TopCenter)
	LayoutSurround11_1 ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | LowFrequency | BackLeft | BackRight | SideLeft | SideRight | FrontLeftOfCenter | FrontRightOfCenter | BackCenter | TopCenter)
	LayoutAtmos11_1_4  ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | LowFrequency | BackLeft | BackRight | SideLeft | SideRight | FrontLeftOfCenter | FrontRightOfCenter | BackCenter | TopCenter | TopFrontLeft | TopFrontRight | TopBackLeft | TopBackRight)
	LayoutAtmos11_1_6  ChannelLayout = NewNativeLayout(FrontLeft | FrontRight | FrontCenter | LowFrequency | BackLeft | BackRight | SideLeft | SideRight | FrontLeftOfCenter | FrontRightOfCenter | BackCenter | TopCenter | TopFrontLeft | TopFrontCenter | TopFrontRight | TopBackLeft | TopBackCenter | TopBackRight)
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

func joinChannelPositions(channels []ChannelPosition) string {
	values := make([]string, len(channels))
	for i, channel := range channels {
		values[i] = channelPositionName(channel)
	}
	return strings.Join(values, ",")
}

func channelPositionName(channel ChannelPosition) string {
	switch channel {
	case FrontLeft:
		return "FL"
	case FrontRight:
		return "FR"
	case FrontCenter:
		return "FC"
	case LowFrequency:
		return "LFE"
	case BackLeft:
		return "BL"
	case BackRight:
		return "BR"
	case FrontLeftOfCenter:
		return "FLC"
	case FrontRightOfCenter:
		return "FRC"
	case BackCenter:
		return "BC"
	case SideLeft:
		return "SL"
	case SideRight:
		return "SR"
	case TopCenter:
		return "TC"
	case TopFrontLeft:
		return "TFL"
	case TopFrontCenter:
		return "TFC"
	case TopFrontRight:
		return "TFR"
	case TopBackLeft:
		return "TBL"
	case TopBackCenter:
		return "TBC"
	case TopBackRight:
		return "TBR"
	default:
		return "0x" + strconv.FormatUint(uint64(channel), 16)
	}
}
