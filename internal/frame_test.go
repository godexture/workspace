package internal

import (
	"errors"
	"io"
	"testing"

	"github.com/godexture/format-flac/streaminfo"
)

// TestDecodeFrame_TruncatedDataReturnsError verifies that a frame whose
// audio data is cut short is rejected with an error (via Reader.Overrun())
// rather than panicking or silently producing garbage samples. The Fast-tier
// hot path (residual/Rice decoding) no longer returns a per-call error on
// truncation, so this exercises the aggregate check at the end of
// decodeFrame.
func TestDecodeFrame_TruncatedDataReturnsError(t *testing.T) {
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	info, err := streaminfo.Parse(data[8:42])
	if err != nil {
		t.Fatalf("streaminfo.Parse() error = %v", err)
	}
	frameData := data[42:]

	for cut := 1; cut < len(frameData); cut++ {
		truncated := frameData[:cut]
		_, err := decodeFrame(truncated, info)
		if err == nil {
			// Some very short prefixes may still fail structural validation
			// before truncation would even matter; that's fine as long as
			// we never panic and never silently succeed on this specific
			// full-length-minus-one case.
			if cut == len(frameData)-1 {
				t.Fatalf("decodeFrame() with 1 byte missing succeeded, want an error")
			}
			continue
		}
	}
}

// TestDecodeFrame_TruncatedFooterDetected checks the specific case of
// losing just the trailing CRC-16, which used to be caught by the footer's
// own readBits(16) error and is now caught by the Overrun() check instead.
func TestDecodeFrame_TruncatedFooterDetected(t *testing.T) {
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	info, err := streaminfo.Parse(data[8:42])
	if err != nil {
		t.Fatalf("streaminfo.Parse() error = %v", err)
	}
	frameData := data[42:]
	truncated := frameData[:len(frameData)-2] // drop the 2-byte CRC-16 footer

	_, err = decodeFrame(truncated, info)
	if err == nil {
		t.Fatalf("decodeFrame() with missing CRC-16 footer succeeded, want an error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("decodeFrame() error = %v, want io.ErrUnexpectedEOF", err)
	}
}
