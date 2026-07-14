package audio

import (
	"bytes"
	"os"
	"testing"
)

// RunRoundtripDemuxMux runs a roundtrip test starting from a file, demuxing it to packets, muxing it back, and demuxing again to verify packet integrity.
func RunRoundtripDemuxMux(t *testing.T, mediaPath string, demux DemuxFunc, mux MuxFunc) {
	t.Helper()

	originalBytes, err := os.ReadFile(mediaPath)
	if err != nil {
		t.Fatalf("failed to read media file: %v", err)
	}

	packets1, err := demux(originalBytes)
	if err != nil {
		t.Fatalf("failed to demux original media: %v", err)
	}

	muxedBytes, err := mux(packets1)
	if err != nil {
		t.Fatalf("failed to mux packets: %v", err)
	}

	packets2, err := demux(muxedBytes)
	if err != nil {
		t.Fatalf("failed to demux intermediate media: %v", err)
	}

	if len(packets1) != len(packets2) {
		t.Fatalf("packet count mismatch: original=%d, muxed=%d", len(packets1), len(packets2))
	}
	for i := range packets1 {
		if !bytes.Equal(packets1[i], packets2[i]) {
			t.Errorf("packet %d mismatch", i)
		}
	}
}

// RunRoundtripMuxDemux runs a roundtrip test starting from generated packets, muxing them, and demuxing them back.
func RunRoundtripMuxDemux(t *testing.T, packets [][]byte, mux MuxFunc, demux DemuxFunc) {
	t.Helper()

	muxedBytes, err := mux(packets)
	if err != nil {
		t.Fatalf("failed to mux packets: %v", err)
	}

	demuxedPackets, err := demux(muxedBytes)
	if err != nil {
		t.Fatalf("failed to demux media: %v", err)
	}

	if len(packets) != len(demuxedPackets) {
		t.Fatalf("packet count mismatch: expected=%d, got=%d", len(packets), len(demuxedPackets))
	}
	for i := range packets {
		if !bytes.Equal(packets[i], demuxedPackets[i]) {
			t.Errorf("packet %d mismatch", i)
		}
	}
}

// RunRoundtripDecodeEncode runs a roundtrip test starting from packets, decoding them, encoding them, and decoding them again.
func RunRoundtripDecodeEncode(t *testing.T, packets [][]byte, opts CompareOptions, decode DecodeFunc, encode EncodeFunc) {
	t.Helper()

	pcm1, err := decode(packets)
	if err != nil {
		t.Fatalf("failed to decode original packets: %v", err)
	}

	encodedPackets, err := encode(pcm1)
	if err != nil {
		t.Fatalf("failed to encode PCM 1: %v", err)
	}

	pcm2, err := decode(encodedPackets)
	if err != nil {
		t.Fatalf("failed to decode intermediate packets: %v", err)
	}

	if err := ComparePCM(pcm2, pcm1, opts); err != nil {
		t.Errorf("PCM degradation check failed for roundtrip: %v", err)
	}
}

// RunRoundtripEncodeDecode runs a roundtrip test starting from PCM array, encoding to packets, and decoding back to PCM.
func RunRoundtripEncodeDecode(t *testing.T, srcPCM []float32, opts CompareOptions, encode EncodeFunc, decode DecodeFunc) {
	t.Helper()

	encodedPackets, err := encode(srcPCM)
	if err != nil {
		t.Fatalf("failed to encode source PCM: %v", err)
	}

	decodedPCM, err := decode(encodedPackets)
	if err != nil {
		t.Fatalf("failed to decode intermediate packets: %v", err)
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

// RunRoundtripDemuxDecodeEncodeMux runs a roundtrip test by demuxing, decoding, encoding, and muxing.
func RunRoundtripDemuxDecodeEncodeMux(t *testing.T, mediaPath string, opts CompareOptions, demux DemuxFunc, decode DecodeFunc, encode EncodeFunc, mux MuxFunc) {
	t.Helper()

	originalBytes, err := os.ReadFile(mediaPath)
	if err != nil {
		t.Fatalf("failed to read media file: %v", err)
	}

	packets1, err := demux(originalBytes)
	if err != nil {
		t.Fatalf("failed to demux original media: %v", err)
	}

	pcm1, err := decode(packets1)
	if err != nil {
		t.Fatalf("failed to decode original packets: %v", err)
	}

	packets2, err := encode(pcm1)
	if err != nil {
		t.Fatalf("failed to encode PCM 1: %v", err)
	}

	muxedBytes, err := mux(packets2)
	if err != nil {
		t.Fatalf("failed to mux packets: %v", err)
	}

	packets3, err := demux(muxedBytes)
	if err != nil {
		t.Fatalf("failed to demux intermediate media: %v", err)
	}

	pcm2, err := decode(packets3)
	if err != nil {
		t.Fatalf("failed to decode intermediate packets: %v", err)
	}

	if len(pcm2) > len(pcm1) {
		pcm2 = pcm2[:len(pcm1)]
	}
	if len(pcm2) < len(pcm1) {
		t.Fatalf("length mismatch: got %d, expected at least %d", len(pcm2), len(pcm1))
	}

	if err := ComparePCM(pcm2, pcm1, opts); err != nil {
		t.Errorf("PCM degradation check failed for roundtrip: %v", err)
	}
}

// RunRoundtripEncodeMuxDemuxDecode runs a roundtrip test by encoding, muxing, demuxing, and decoding.
func RunRoundtripEncodeMuxDemuxDecode(t *testing.T, srcPCM []float32, opts CompareOptions, encode EncodeFunc, mux MuxFunc, demux DemuxFunc, decode DecodeFunc) {
	t.Helper()

	packets, err := encode(srcPCM)
	if err != nil {
		t.Fatalf("failed to encode source PCM: %v", err)
	}

	muxedBytes, err := mux(packets)
	if err != nil {
		t.Fatalf("failed to mux packets: %v", err)
	}

	demuxedPackets, err := demux(muxedBytes)
	if err != nil {
		t.Fatalf("failed to demux media: %v", err)
	}

	decodedPCM, err := decode(demuxedPackets)
	if err != nil {
		t.Fatalf("failed to decode intermediate packets: %v", err)
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
