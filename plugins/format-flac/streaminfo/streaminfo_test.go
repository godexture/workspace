package streaminfo

import (
	"testing"
	"time"
)

func TestEncodeParseRoundTrip(t *testing.T) {
	t.Parallel()
	want := StreamInfo{
		MinBlockSize:  4096,
		MaxBlockSize:  8192,
		MinFrameSize:  123,
		MaxFrameSize:  4567,
		SampleRate:    384000,
		Channels:      2,
		BitsPerSample: 24,
		TotalSamples:  (1 << 36) - 1,
		MD5:           [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
	}

	got, err := Parse(Encode(want))
	if err != nil {
		t.Fatalf("Parse(Encode()) error = %v", err)
	}
	if got != want {
		t.Fatalf("Parse(Encode()) = %#v, want %#v", got, want)
	}
}

func TestStreamInfoDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info StreamInfo
		want time.Duration
	}{
		{
			name: "unknown total samples",
			info: StreamInfo{SampleRate: 44100},
		},
		{
			name: "unknown sample rate",
			info: StreamInfo{TotalSamples: 44100},
		},
		{
			name: "whole second",
			info: StreamInfo{TotalSamples: 44100, SampleRate: 44100},
			want: time.Second,
		},
		{
			name: "fractional sample",
			info: StreamInfo{TotalSamples: 1, SampleRate: 44100},
			want: 22675,
		},
		{
			name: "duration overflow",
			info: StreamInfo{TotalSamples: (1 << 36) - 1, SampleRate: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.Duration(); got != tt.want {
				t.Fatalf("Duration() = %v, want %v", got, tt.want)
			}
		})
	}
}
