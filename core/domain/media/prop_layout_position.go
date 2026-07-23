package media

import (
	"strconv"
	"strings"
)

type ChannelPosition uint64

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
