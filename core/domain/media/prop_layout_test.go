package media

import "testing"

func TestPredefinedChannelLayoutsAreValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		layout   ChannelLayout
		channels int
		lfe      int
	}{
		{"mono", LayoutMono1, 1, 0},
		{"dual-mono", LayoutDualMono2, 2, 0},
		{"stereo-2.0", LayoutStereo2_0, 2, 0},
		{"stereo-2.1", LayoutStereo2_1, 3, 1},
		{"stereo-3.0", LayoutStereo3_0, 3, 0},
		{"stereo-3.1", LayoutStereo3_1, 4, 1},
		{"surround-3.0", LayoutSurround3_0, 3, 0},
		{"quad-4.0", LayoutQuad4_0, 4, 0},
		{"side-quad-4.0", LayoutSideQuad4_0, 4, 0},
		{"surround-4.0", LayoutSurround4_0, 4, 0},
		{"surround-4.1", LayoutSurround4_1, 5, 1},
		{"front-5.0", LayoutFront5_0, 5, 0},
		{"front-5.1", LayoutFront5_1, 6, 1},
		{"side-5.0", LayoutSide5_0, 5, 0},
		{"side-5.1", LayoutSide5_1, 6, 1},
		{"atmos-5.1.4", LayoutAtmos5_1_4, 10, 1},
		{"hexagonal-6.0", LayoutHexagonal6_0, 6, 0},
		{"hexagonal-6.1", LayoutHexagonal6_1, 7, 1},
		{"front-6.0", LayoutFront6_0, 6, 0},
		{"side-6.0", LayoutSide6_0, 6, 0},
		{"side-6.1", LayoutSide6_1, 7, 1},
		{"wide-7.1", LayoutWide7_1, 8, 1},
		{"side-7.0", LayoutSide7_0, 7, 0},
		{"side-7.1", LayoutSide7_1, 8, 1},
		{"surround-7.1", LayoutSurround7_1, 8, 1},
		{"atmos-7.1.4", LayoutAtmos7_1_4, 12, 1},
		{"octagonal-8.0", LayoutOctagonal8_0, 8, 0},
		{"surround-9.0", LayoutSurround9_0, 9, 0},
		{"surround-9.1", LayoutSurround9_1, 10, 1},
		{"surround-11.0", LayoutSurround11_0, 11, 0},
		{"surround-11.1", LayoutSurround11_1, 12, 1},
		{"atmos-11.1.4", LayoutAtmos11_1_4, 16, 1},
		{"atmos-11.1.6", LayoutAtmos11_1_6, 18, 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.layout.Validate(); err != nil {
				t.Fatal(err)
			}
			if got := test.layout.ChannelCount(); got != test.channels {
				t.Fatalf("channel count = %d, want %d", got, test.channels)
			}
			if got := countChannel(test.layout.Enumerate(), LowFrequency); got != test.lfe {
				t.Fatalf("LFE count = %d, want %d", got, test.lfe)
			}
		})
	}
}

func TestCustomLayoutIsValueBased(t *testing.T) {
	t.Parallel()
	first := NewCustomLayout(FrontCenter, FrontCenter, SideLeft)
	second := NewCustomLayout(FrontCenter, FrontCenter, SideLeft)
	if first != second {
		t.Fatal("equal custom mappings must have equal layout values")
	}
	if got, want := first.String(), "custom(FC,FC,SL)"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestWideSevenOneUsesFrontWideChannels(t *testing.T) {
	t.Parallel()
	if !LayoutWide7_1.Contains(FrontLeftOfCenter) || !LayoutWide7_1.Contains(FrontRightOfCenter) {
		t.Fatal("wide 7.1 layout is missing front-wide channels")
	}
	if LayoutWide7_1.Contains(SideLeft) || LayoutWide7_1.Contains(SideRight) {
		t.Fatal("wide 7.1 layout must not use side channels")
	}
}

func TestAmbisonicLayoutDoesNotOverflow(t *testing.T) {
	t.Parallel()
	layout := NewAmbisonicLayout(15)
	if err := layout.Validate(); err != nil {
		t.Fatal(err)
	}
	if got, want := layout.ChannelCount(), 256; got != want {
		t.Fatalf("channel count = %d, want %d", got, want)
	}
}

func TestParseChannelLayout(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]ChannelLayout{
		"mono":   LayoutMono1,
		"1.0":    LayoutMono1,
		"stereo": LayoutStereo2_0,
		"2.0":    LayoutStereo2_0,
		"quad":   LayoutQuad4_0,
	} {
		got, err := ParseChannelLayout(input)
		if err != nil {
			t.Fatalf("ParseChannelLayout(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseChannelLayout(%q) = %s, want %s", input, got, want)
		}
	}
	if _, err := ParseChannelLayout("not-a-layout"); err == nil {
		t.Fatal("ParseChannelLayout() accepted an unknown layout")
	}
}

func countChannel(channels []ChannelPosition, want ChannelPosition) int {
	count := 0
	for _, channel := range channels {
		if channel == want {
			count++
		}
	}
	return count
}
