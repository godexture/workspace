package audio

import (
	"bytes"
	"os"
	"testing"
)

// RunSnapshotDecode runs a decoding snapshot test.
func RunSnapshotDecode(t *testing.T, expectedPCM []float32, mediaPath string, opts CompareOptions, decode func([]byte) ([]float32, error)) {
	t.Helper()

	mediaBytes, err := os.ReadFile(mediaPath)
	if err != nil {
		t.Fatalf("failed to read media file: %v", err)
	}

	actualPCM, err := decode(mediaBytes)
	if err != nil {
		t.Fatalf("failed to decode media data: %v", err)
	}

	if err := ComparePCM(actualPCM, expectedPCM, opts); err != nil {
		t.Errorf("PCM comparison failed: %v", err)
	}
}

func RunSnapshotEncode(t *testing.T, sourcePCM []float32, opts CompareOptions, encode func([]float32) ([]byte, error)) {
	t.Helper()

	encodedBytes, err := encode(sourcePCM)
	if err != nil {
		t.Fatalf("failed to encode PCM: %v", err)
	}

	decodedPCM, err := DecodeWithFFmpeg(bytes.NewReader(encodedBytes))
	if err != nil {
		t.Fatalf("failed to decode encoded bytes: %v", err)
	}

	if err := ComparePCM(decodedPCM, sourcePCM, opts); err != nil {
		t.Errorf("PCM comparison failed: %v", err)
	}
}

// RunRoundtripDecodeEncode runs a roundtrip test starting from a file, decoding it, encoding it, and decoding it again.
func RunRoundtripDecodeEncode(t *testing.T, mediaPath string, opts CompareOptions, decode func([]byte) ([]float32, error), encode func([]float32) ([]byte, error)) {
	t.Helper()

	originalBytes, err := os.ReadFile(mediaPath)
	if err != nil {
		t.Fatalf("failed to read media file: %v", err)
	}

	pcm1, err := decode(originalBytes)
	if err != nil {
		t.Fatalf("failed to decode original media: %v", err)
	}

	encodedBytes, err := encode(pcm1)
	if err != nil {
		t.Fatalf("failed to encode PCM 1: %v", err)
	}

	pcm2, err := decode(encodedBytes)
	if err != nil {
		t.Fatalf("failed to decode intermediate media: %v", err)
	}

	if err := ComparePCM(pcm2, pcm1, opts); err != nil {
		t.Errorf("PCM degradation check failed for roundtrip: %v", err)
	}
}

// RunRoundtripEncodeDecode runs a roundtrip test starting from a generated PCM array, encoding it, and decoding it.
func RunRoundtripEncodeDecode(t *testing.T, srcPCM []float32, opts CompareOptions, encode func([]float32) ([]byte, error), decode func([]byte) ([]float32, error)) {
	t.Helper()

	encodedBytes, err := encode(srcPCM)
	if err != nil {
		t.Fatalf("failed to encode source PCM: %v", err)
	}

	decodedPCM, err := decode(encodedBytes)
	if err != nil {
		t.Fatalf("failed to decode intermediate bytes: %v", err)
	}

	if len(decodedPCM) > len(srcPCM) {
		decodedPCM = decodedPCM[:len(srcPCM)]
	}
	if len(decodedPCM) < len(srcPCM) {
		t.Fatalf("length mismatch: got %d, expected at least %d", len(decodedPCM), len(srcPCM))
	}

	if err := ComparePCM(decodedPCM, srcPCM, opts); err != nil {
		t.Errorf("PCM degradation check failed for roundtrip: %v", err)
	}
}
