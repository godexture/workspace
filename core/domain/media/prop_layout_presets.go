package media

import (
	"fmt"
	"strings"
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

var namedChannelLayouts = map[string]ChannelLayout{
	"mono":   LayoutMono1,
	"1":      LayoutMono1,
	"1.0":    LayoutMono1,
	"stereo": LayoutStereo2_0,
	"2":      LayoutStereo2_0,
	"2.0":    LayoutStereo2_0,
	"quad":   LayoutQuad4_0,
	"4.0":    LayoutQuad4_0,
	"5.1":    LayoutFront5_1,
	"7.1":    LayoutSurround7_1,
}

func ParseChannelLayout(value string) (ChannelLayout, error) {
	name := strings.ToLower(strings.TrimSpace(value))
	layout, ok := namedChannelLayouts[name]
	if !ok {
		return ChannelLayout{}, fmt.Errorf("unknown channel layout %q; use mono, stereo, quad, 5.1, or 7.1", value)
	}
	return layout, nil
}

func (l *ChannelLayout) UnmarshalText(value []byte) error {
	if l == nil {
		return fmt.Errorf("channel layout destination is nil")
	}
	parsed, err := ParseChannelLayout(string(value))
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}
