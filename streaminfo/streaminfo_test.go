package streaminfo

import "testing"

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
