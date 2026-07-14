package audio

import (
	"bytes"
	"os"
	"testing"
)

// RunSnapshotDemuxDecode runs a decoding snapshot test.
func RunSnapshotDemuxDecode(t *testing.T, expectedPCM []float32, mediaPath string, opts CompareOptions, demux DemuxFunc, decode DecodeFunc) {
	t.Helper()

	mediaBytes, err := os.ReadFile(mediaPath)
	if err != nil {
		t.Fatalf("failed to read media file: %v", err)
	}

	packets, err := demux(mediaBytes)
	if err != nil {
		t.Fatalf("failed to demux media data: %v", err)
	}

	actualPCM, err := decode(packets)
	if err != nil {
		t.Fatalf("failed to decode media data: %v", err)
	}

	if err := ComparePCM(actualPCM, expectedPCM, opts); err != nil {
		t.Errorf("PCM comparison failed: %v", err)
	}
}

func RunSnapshotEncodeMux(t *testing.T, sourcePCM []float32, opts CompareOptions, encode EncodeFunc, mux MuxFunc) {
	t.Helper()

	encodedBytes, err := encode(sourcePCM)
	if err != nil {
		t.Fatalf("failed to encode PCM: %v", err)
	}

	muxedBytes, err := mux(encodedBytes)
	if err != nil {
		t.Fatalf("failed to mux encoded bytes: %v", err)
	}

	decodedPCM, err := DecodeWithFFmpeg(bytes.NewReader(muxedBytes))
	if err != nil {
		t.Fatalf("failed to decode encoded bytes: %v", err)
	}

	if err := ComparePCM(decodedPCM, sourcePCM, opts); err != nil {
		t.Errorf("PCM comparison failed: %v", err)
	}
}
