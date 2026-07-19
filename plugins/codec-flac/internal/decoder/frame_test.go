package decoder

import (
	"errors"
	"io"
	"testing"

	"github.com/godexture/format-flac/streaminfo"
)

func TestDecodeFrame_TruncatedDataReturnsError(t *testing.T) {
	t.Parallel()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	info, err := streaminfo.Parse(data[8:42])
	if err != nil {
		t.Fatalf("streaminfo.Parse() error = %v", err)
	}
	frameData := data[42:]

	for cut := 1; cut < len(frameData); cut++ {
		truncated := frameData[:cut]
		_, err := DecodeFrame(truncated, info)
		if err == nil {
			if cut == len(frameData)-1 {
				t.Fatalf("DecodeFrame() with 1 byte missing succeeded, want an error")
			}
			continue
		}
	}
}

func TestDecodeFrame_TruncatedFooterDetected(t *testing.T) {
	t.Parallel()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	info, err := streaminfo.Parse(data[8:42])
	if err != nil {
		t.Fatalf("streaminfo.Parse() error = %v", err)
	}
	frameData := data[42:]
	truncated := frameData[:len(frameData)-2]

	_, err = DecodeFrame(truncated, info)
	if err == nil {
		t.Fatalf("DecodeFrame() with missing CRC-16 footer succeeded, want an error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("DecodeFrame() error = %v, want io.ErrUnexpectedEOF", err)
	}
}
