package internal

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"testing"
	"time"

	mp3codec "github.com/godexture/codec-mp3"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/format-wav/params"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil"
)

func TestProberCognizesWAVSignature(t *testing.T) {
	t.Parallel()
	data := buildTestWAV(t, []byte{0x01, 0x02, 0x03, 0x04})

	if got := Probe(bytes.NewReader(data)); got != 100 {
		t.Fatalf("Probe() = %d, want %d", got, 100)
	}
}

func TestWAVRoundTripMonoPCM16(t *testing.T) {
	t.Parallel()
	original := []byte{0x10, 0x00, 0x20, 0x00, 0x30, 0x00, 0x40, 0x00}
	wavData := buildTestWAV(t, original)

	demuxer, err := NewDemuxer(bytes.NewReader(wavData), DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	streams, _, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if len(streams) != 1 {
		t.Fatalf("Analyze() returned %d streams, want 1", len(streams))
	}

	stream := streams[0]
	if stream.Type != media.MediaAudio {
		t.Fatalf("stream.Type = %s, want %s", stream.Type, media.MediaAudio)
	}
	if stream.MediaAttributes.Audio.Format != media.SampleFormatS16 {
		t.Fatalf("stream format = %s, want %s", stream.MediaAttributes.Audio.Format, media.SampleFormatS16)
	}
	if stream.MediaAttributes.Audio.SampleRate != 48000 {
		t.Fatalf("stream sample rate = %d, want %d", stream.MediaAttributes.Audio.SampleRate, 48000)
	}
	if stream.MediaAttributes.Audio.ChannelLayout.ChannelCount() != 1 {
		t.Fatalf("stream channels = %d, want 1", stream.MediaAttributes.Audio.ChannelLayout.ChannelCount())
	}

	pkt, streamIndex, err := demuxer.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket() error = %v", err)
	}
	if streamIndex != 0 {
		t.Fatalf("streamIndex = %d, want 0", streamIndex)
	}
	if !bytes.Equal(pkt.Data(), original) {
		t.Fatalf("packet data mismatch: got %v, want %v", pkt.Data(), original)
	}

	out := testutil.NewBuffer(nil)
	muxer := NewMuxer(out, MuxerConfig{})
	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket() error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	if !bytes.Equal(out.Bytes(), wavData) {
		t.Fatalf("muxed wav mismatch: got %d bytes, want %d bytes", len(out.Bytes()), len(wavData))
	}
}

func TestWAVRoundTripPCM24(t *testing.T) {
	t.Parallel()
	// 24-bit PCM (3 bytes per sample). Let's do 2 channels. 3 samples each = 18 bytes.
	original := []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, // sample 1 (L, R)
		0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, // sample 2 (L, R)
		0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, // sample 3 (L, R)
	}

	attr := media.MediaAttributes{
		Codec: media.CodecLPCM,
		Audio: media.AudioAttributes{
			SampleRate:    44100,
			Format:        media.SampleFormatS24,
			ChannelLayout: media.LayoutStereo2_0,
		},
	}
	wavData := buildTestWAVWithAttr(t, original, attr)

	demuxer, err := NewDemuxer(bytes.NewReader(wavData), DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	streams, _, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if len(streams) != 1 {
		t.Fatalf("Analyze() returned %d streams, want 1", len(streams))
	}

	stream := streams[0]
	if stream.Type != media.MediaAudio {
		t.Fatalf("stream.Type = %s, want %s", stream.Type, media.MediaAudio)
	}
	if stream.MediaAttributes.Audio.Format != media.SampleFormatS24 {
		t.Fatalf("stream format = %s, want %s", stream.MediaAttributes.Audio.Format, media.SampleFormatS24)
	}
	if stream.MediaAttributes.Audio.SampleRate != 44100 {
		t.Fatalf("stream sample rate = %d, want %d", stream.MediaAttributes.Audio.SampleRate, 44100)
	}
	if stream.MediaAttributes.Audio.ChannelLayout.ChannelCount() != 2 {
		t.Fatalf("stream channels = %d, want 2", stream.MediaAttributes.Audio.ChannelLayout.ChannelCount())
	}

	pkt, streamIndex, err := demuxer.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket() error = %v", err)
	}
	if streamIndex != 0 {
		t.Fatalf("streamIndex = %d, want 0", streamIndex)
	}
	if !bytes.Equal(pkt.Data(), original) {
		t.Fatalf("packet data mismatch: got %v, want %v", pkt.Data(), original)
	}

	out := testutil.NewBuffer(nil)
	muxer := NewMuxer(out, MuxerConfig{})
	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket() error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	if !bytes.Equal(out.Bytes(), wavData) {
		t.Fatalf("muxed wav mismatch: got %d bytes, want %d bytes", len(out.Bytes()), len(wavData))
	}
}

func buildTestWAV(t *testing.T, payload []byte) []byte {
	return buildTestWAVWithAttr(t, payload, media.MediaAttributes{
		Codec: media.CodecLPCM,
		Audio: media.AudioAttributes{
			SampleRate:    48000,
			Format:        media.SampleFormatS16,
			ChannelLayout: media.LayoutMono1,
		},
	})
}

func buildTestWAVWithAttr(t *testing.T, payload []byte, attr media.MediaAttributes) []byte {
	t.Helper()

	out := testutil.NewBuffer(nil)
	muxer := NewMuxer(out, MuxerConfig{})
	stream := media.StreamInfo{
		Type:            media.MediaAudio,
		MediaAttributes: attr,
	}

	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}

	pkt := media.NewPacket(len(payload))
	copy(pkt.Data(), payload)
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket() error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	return out.Bytes()
}

func TestRF64RoundTrip(t *testing.T) {
	t.Parallel()
	attr := media.MediaAttributes{
		Codec: media.CodecLPCM,
		Audio: media.AudioAttributes{
			SampleRate:    48000,
			Format:        media.SampleFormatS16,
			ChannelLayout: media.LayoutMono1,
		},
	}

	// Simulate 5 GB data
	fakeSize := uint64(5 * 1024 * 1024 * 1024)
	headerBytes, err := buildWAVHeader(attr, fakeSize, 0, false)
	if err != nil {
		t.Fatalf("buildWAVHeader() error = %v", err)
	}

	// Verify it produced a valid RF64 header
	if len(headerBytes) < 12 {
		t.Fatalf("produced data is too short: %d bytes", len(headerBytes))
	}
	if string(headerBytes[0:4]) != "RF64" {
		t.Fatalf("expected RF64 signature, got %q", string(headerBytes[0:4]))
	}

	// Parse it back using Demuxer
	demuxer, err := NewDemuxer(bytes.NewReader(headerBytes), DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	streams, _, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if len(streams) != 1 {
		t.Fatalf("Analyze() returned %d streams, want 1", len(streams))
	}

	parsedStream := streams[0]
	if parsedStream.MediaAttributes.Audio.Format != media.SampleFormatS16 {
		t.Fatalf("stream format = %s, want %s", parsedStream.MediaAttributes.Audio.Format, media.SampleFormatS16)
	}

	if demuxer.header.dataSize != fakeSize {
		t.Fatalf("demuxer parsed dataSize = %d, want %d", demuxer.header.dataSize, fakeSize)
	}
}

func TestReadPacketMemoryLimit(t *testing.T) {
	t.Parallel()
	// Setup a mock demuxer with extremely large dataSize
	demuxer := &Demuxer{
		r: bytes.NewReader(nil),
		header: wavHeader{
			dataSize: uint64(int(^uint(0)>>1)) + 1,
		},
		parsed: true,
	}

	_, _, err := demuxer.ReadPacket()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "wav data chunk is too large for memory" {
		t.Fatalf("expected memory limit error, got %v", err)
	}
}

func TestProbeRecognizesRF64Signature(t *testing.T) {
	t.Parallel()
	attr := media.MediaAttributes{
		Codec: media.CodecLPCM,
		Audio: media.AudioAttributes{
			SampleRate:    48000,
			Format:        media.SampleFormatS16,
			ChannelLayout: media.LayoutMono1,
		},
	}

	// Force RF64
	fakeSize := uint64(5 * 1024 * 1024 * 1024)
	headerBytes, err := buildWAVHeader(attr, fakeSize, 0, false)
	if err != nil {
		t.Fatalf("buildWAVHeader() error = %v", err)
	}

	// Probe the output
	probeResult := Probe(bytes.NewReader(headerBytes))
	if probeResult != 100 {
		t.Fatalf("Probe() returned %v, want 100 (ProbeExactSignature)", probeResult)
	}
}

func TestWAVRoundTripFloat32(t *testing.T) {
	t.Parallel()
	// 1 channel, Float32. This should trigger writeFact (since F32 is non-PCM).
	original := []byte{
		0x00, 0x00, 0x80, 0x3f, // 1.0f
		0x00, 0x00, 0x00, 0x40, // 2.0f
	}

	out := testutil.NewBuffer(nil)
	muxer := NewMuxer(out, MuxerConfig{})
	stream := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecLPCM,
			Audio: media.AudioAttributes{
				SampleRate:    48000,
				Format:        media.SampleFormatF32,
				ChannelLayout: media.LayoutMono1,
			},
		},
	}

	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}

	pkt := media.NewPacket(len(original))
	copy(pkt.Data(), original)
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket() error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	wavData := out.Bytes()

	// Verify that the wavData contains "fact" chunk.
	if !bytes.Contains(wavData, []byte("fact")) {
		t.Errorf("expected WAV header to contain 'fact' chunk for Float32, but it did not")
	}

	// Now demux it and verify
	demuxer, err := NewDemuxer(bytes.NewReader(wavData), DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}
	streams, _, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if len(streams) != 1 {
		t.Fatalf("Analyze() returned %d streams, want 1", len(streams))
	}

	demuxedStream := streams[0]
	if demuxedStream.MediaAttributes.Audio.Format != media.SampleFormatF32 {
		t.Fatalf("stream format = %s, want %s", demuxedStream.MediaAttributes.Audio.Format, media.SampleFormatF32)
	}

	// Verify demuxer header parsed numSamples correctly
	if demuxer.header.numSamples != 2 {
		t.Errorf("demuxer header.numSamples = %d, want 2", demuxer.header.numSamples)
	}

	demuxedPkt, _, err := demuxer.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket() error = %v", err)
	}
	if !bytes.Equal(demuxedPkt.Data(), original) {
		t.Fatalf("packet data mismatch: got %v, want %v", demuxedPkt.Data(), original)
	}
}

func TestRF64Demuxer(t *testing.T) {
	t.Parallel()
	// Construct a minimal RF64 WAV file manually.
	var buf bytes.Buffer
	buf.WriteString("RF64")
	binary.Write(&buf, binary.LittleEndian, uint32(0xFFFFFFFF))
	buf.WriteString("WAVE")

	// ds64
	buf.WriteString("ds64")
	binary.Write(&buf, binary.LittleEndian, uint32(28))
	binary.Write(&buf, binary.LittleEndian, uint64(1000)) // riffSize
	binary.Write(&buf, binary.LittleEndian, uint64(100))  // dataSize
	binary.Write(&buf, binary.LittleEndian, uint64(50))   // numSamples
	binary.Write(&buf, binary.LittleEndian, uint32(0))    // tableLength

	// fmt
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, uint16(1))     // format PCM
	binary.Write(&buf, binary.LittleEndian, uint16(1))     // channels
	binary.Write(&buf, binary.LittleEndian, uint32(48000)) // sampleRate
	binary.Write(&buf, binary.LittleEndian, uint32(96000)) // byteRate
	binary.Write(&buf, binary.LittleEndian, uint16(2))     // blockAlign
	binary.Write(&buf, binary.LittleEndian, uint16(16))    // bitsPerSample

	// data
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(0xFFFFFFFF))
	buf.Write(make([]byte, 100))

	demuxer, err := NewDemuxer(bytes.NewReader(buf.Bytes()), DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	streams, _, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if len(streams) != 1 {
		t.Fatalf("Analyze() returned %d streams, want 1", len(streams))
	}

	if demuxer.header.dataSize != 100 {
		t.Errorf("header.dataSize = %d, want 100", demuxer.header.dataSize)
	}

	if demuxer.header.numSamples != 50 {
		t.Errorf("header.numSamples = %d, want 50", demuxer.header.numSamples)
	}

	pkt, _, err := demuxer.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket() error = %v", err)
	}

	if len(pkt.Data()) != 100 {
		t.Errorf("packet data len = %d, want 100", len(pkt.Data()))
	}
}

func TestWAVRoundTripExtensible5_1_24Bit(t *testing.T) {
	t.Parallel()
	// 5.1 layout has 6 channels. 24-bit PCM.
	// 6 channels * 3 bytes/sample = 18 bytes per sample frame.
	// Let's write 2 sample frames = 36 bytes.
	original := make([]byte, 36)
	for i := range original {
		original[i] = byte(i)
	}

	attr := media.MediaAttributes{
		Codec: media.CodecLPCM,
		Audio: media.AudioAttributes{
			SampleRate:    48000,
			Format:        media.SampleFormatS24,
			ChannelLayout: media.LayoutFront5_1,
		},
	}
	wavData := buildTestWAVWithAttr(t, original, attr)

	// Verify that the wavData contains the wave format extensible tag (0xFFFE).
	// The wavTagFmt is "fmt " (4 bytes) followed by chunk size (4 bytes) and then format tag (2 bytes).
	// So format tag starts 8 bytes after "fmt ".
	fmtIdx := bytes.Index(wavData, []byte("fmt "))
	if fmtIdx == -1 {
		t.Fatalf("expected WAV data to contain 'fmt ' chunk")
	}
	formatTag := binary.LittleEndian.Uint16(wavData[fmtIdx+8 : fmtIdx+10])
	if formatTag != 0xFFFE {
		t.Errorf("expected format tag to be 0xFFFE (WAVEFORMATEXTENSIBLE), got 0x%x", formatTag)
	}

	demuxer, err := NewDemuxer(bytes.NewReader(wavData), DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	streams, _, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if len(streams) != 1 {
		t.Fatalf("Analyze() returned %d streams, want 1", len(streams))
	}

	stream := streams[0]
	if stream.Type != media.MediaAudio {
		t.Fatalf("stream.Type = %s, want %s", stream.Type, media.MediaAudio)
	}
	if stream.MediaAttributes.Audio.Format != media.SampleFormatS24 {
		t.Fatalf("stream format = %s, want %s", stream.MediaAttributes.Audio.Format, media.SampleFormatS24)
	}
	if stream.MediaAttributes.Audio.SampleRate != 48000 {
		t.Fatalf("stream sample rate = %d, want %d", stream.MediaAttributes.Audio.SampleRate, 48000)
	}
	if stream.MediaAttributes.Audio.ChannelLayout.ChannelCount() != 6 {
		t.Fatalf("stream channels = %d, want 6", stream.MediaAttributes.Audio.ChannelLayout.ChannelCount())
	}
	if stream.MediaAttributes.Audio.ChannelLayout.Mask() != media.LayoutFront5_1.Mask() {
		t.Fatalf("stream channel mask = 0x%x, want 0x%x", stream.MediaAttributes.Audio.ChannelLayout.Mask(), media.LayoutFront5_1.Mask())
	}

	pkt, streamIndex, err := demuxer.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket() error = %v", err)
	}
	if streamIndex != 0 {
		t.Fatalf("streamIndex = %d, want 0", streamIndex)
	}
	if !bytes.Equal(pkt.Data(), original) {
		t.Fatalf("packet data mismatch: got %v, want %v", pkt.Data(), original)
	}
}

func TestWAVRoundTripExtensibleCustomLayout(t *testing.T) {
	t.Parallel()
	// Let's create a custom layout: 1 channel but mapped to FrontRight (0x2)
	// which is different from default LayoutMono1 (0x4).
	customLayout := media.NewNativeLayout(media.FrontRight)
	original := []byte{0x10, 0x20, 0x30, 0x40} // 16-bit PCM, 2 samples -> 4 bytes
	attr := media.MediaAttributes{
		Codec: media.CodecLPCM,
		Audio: media.AudioAttributes{
			SampleRate:    48000,
			Format:        media.SampleFormatS16,
			ChannelLayout: customLayout,
		},
	}
	wavData := buildTestWAVWithAttr(t, original, attr)

	fmtIdx := bytes.Index(wavData, []byte("fmt "))
	if fmtIdx == -1 {
		t.Fatalf("expected WAV data to contain 'fmt ' chunk")
	}
	formatTag := binary.LittleEndian.Uint16(wavData[fmtIdx+8 : fmtIdx+10])
	if formatTag != 0xFFFE {
		t.Errorf("expected format tag to be 0xFFFE (WAVEFORMATEXTENSIBLE), got 0x%x", formatTag)
	}

	demuxer, err := NewDemuxer(bytes.NewReader(wavData), DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	streams, _, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	stream := streams[0]
	if stream.MediaAttributes.Audio.ChannelLayout.ChannelCount() != 1 {
		t.Fatalf("stream channels = %d, want 1", stream.MediaAttributes.Audio.ChannelLayout.ChannelCount())
	}
	if stream.MediaAttributes.Audio.ChannelLayout.Mask() != customLayout.Mask() {
		t.Fatalf("stream channel mask = 0x%x, want 0x%x", stream.MediaAttributes.Audio.ChannelLayout.Mask(), customLayout.Mask())
	}
}

func TestWAVMuxerNonSeekable(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	muxer := NewMuxer(&out, MuxerConfig{})
	stream := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecLPCM,
			Audio: media.AudioAttributes{
				SampleRate:    48000,
				Format:        media.SampleFormatS16,
				ChannelLayout: media.LayoutMono1,
			},
		},
	}

	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}

	pkt := media.NewPacket(4)
	copy(pkt.Data(), []byte{0x01, 0x02, 0x03, 0x04})
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket() error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	wavData := out.Bytes()
	if len(wavData) < 44 {
		t.Fatalf("produced WAV data is too short: %d bytes", len(wavData))
	}
	riffSize := binary.LittleEndian.Uint32(wavData[4:8])
	if riffSize != 0xFFFFFFFF {
		t.Errorf("expected RIFF size to be 0xFFFFFFFF, got 0x%x", riffSize)
	}
	dataIdx := bytes.Index(wavData, []byte("data"))
	if dataIdx == -1 {
		t.Fatalf("expected data chunk")
	}
	dataSize := binary.LittleEndian.Uint32(wavData[dataIdx+4 : dataIdx+8])
	if dataSize != 0xFFFFFFFF {
		t.Errorf("expected data chunk size to be 0xFFFFFFFF, got 0x%x", dataSize)
	}
}

func TestWAVMuxerforceRF64(t *testing.T) {
	t.Parallel()
	out := testutil.NewBuffer(nil)
	muxer := NewMuxer(out, MuxerConfig{ForceRF64: true})
	stream := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecLPCM,
			Audio: media.AudioAttributes{
				SampleRate:    48000,
				Format:        media.SampleFormatS16,
				ChannelLayout: media.LayoutMono1,
			},
		},
	}

	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}

	pkt := media.NewPacket(4)
	copy(pkt.Data(), []byte{0x01, 0x02, 0x03, 0x04})
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket() error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	wavData := out.Bytes()
	if len(wavData) < 12 {
		t.Fatalf("produced RF64 WAV data is too short: %d bytes", len(wavData))
	}
	if string(wavData[0:4]) != "RF64" {
		t.Errorf("expected RF64 signature, got %q", string(wavData[0:4]))
	}

	ds64Idx := bytes.Index(wavData, []byte("ds64"))
	if ds64Idx == -1 {
		t.Fatalf("expected ds64 chunk")
	}
	riffSize64 := binary.LittleEndian.Uint64(wavData[ds64Idx+8 : ds64Idx+16])
	dataSize64 := binary.LittleEndian.Uint64(wavData[ds64Idx+16 : ds64Idx+24])
	numSamples64 := binary.LittleEndian.Uint64(wavData[ds64Idx+24 : ds64Idx+32])

	expectedDataSize := uint64(4)
	if dataSize64 != expectedDataSize {
		t.Errorf("expected dataSize in ds64 to be %d, got %d", expectedDataSize, dataSize64)
	}
	if numSamples64 != 2 {
		t.Errorf("expected numSamples in ds64 to be 2, got %d", numSamples64)
	}

	expectedRiffSize := uint64(76)
	if riffSize64 != expectedRiffSize {
		t.Errorf("expected riffSize in ds64 to be %d, got %d", expectedRiffSize, riffSize64)
	}
}

func TestWAVDemuxerSeek(t *testing.T) {
	t.Parallel()

	// Create a WAV with 48000 Hz, 16-bit, Mono. 1 second of data.
	// 48000 samples * 2 bytes = 96000 bytes
	payload := make([]byte, 96000)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	attr := media.MediaAttributes{
		Codec: media.CodecLPCM,
		Audio: media.AudioAttributes{
			SampleRate:    48000,
			Format:        media.SampleFormatS16,
			ChannelLayout: media.LayoutMono1,
		},
	}
	wavData := buildTestWAVWithAttr(t, payload, attr)

	demuxer, err := NewDemuxer(bytes.NewReader(wavData), DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	// Seek to 0.5 seconds
	if err := demuxer.Seek(500 * time.Millisecond); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}

	// 0.5s = 24000 samples = 48000 bytes.
	// We expect the next read packet to start from offset 48000 of the original payload.
	pkt, _, err := demuxer.ReadPacket()
	if err != nil && err != io.EOF {
		t.Fatalf("ReadPacket() error = %v", err)
	}

	if len(pkt.Data()) == 0 {
		t.Fatalf("ReadPacket() returned empty packet")
	}

	if pkt.Data()[0] != payload[48000] {
		t.Errorf("expected first byte to be %v, got %v", payload[48000], pkt.Data()[0])
	}
}

func TestWAVDemuxerSeekADPCM(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		codec    media.CodecID
		channels int
	}{
		{"MS ADPCM Mono", media.CodecMSADPCM, 1},
		{"MS ADPCM Stereo", media.CodecMSADPCM, 2},
		{"IMA ADPCM Mono", media.CodecIMAADPCM, 1},
		{"IMA ADPCM Stereo", media.CodecIMAADPCM, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			blockAlign := 256 * tt.channels
			// Create a 10-block payload
			original := make([]byte, blockAlign*10)
			for i := range original {
				original[i] = byte(i % 256)
			}

			attr := media.MediaAttributes{
				Codec: tt.codec,
				Audio: media.AudioAttributes{
					SampleRate:    8000,
					Format:        media.SampleFormatUnknown,
					ChannelLayout: layoutFromChannelCount(tt.channels),
				},
			}

			wavData := buildTestWAVWithAttr(t, original, attr)

			demuxer, err := NewDemuxer(bytes.NewReader(wavData), DemuxerConfig{})
			if err != nil {
				t.Fatalf("NewDemuxer() error = %v", err)
			}

			// We need to call Analyze/read first to make sure stream is parsed
			streams, _, err := demuxer.Analyze()
			if err != nil {
				t.Fatalf("Analyze() error = %v", err)
			}
			if len(streams) != 1 {
				t.Fatalf("Analyze() returned %d streams, want 1", len(streams))
			}

			// Determine samplesPerBlock
			var samplesPerBlock int
			if tt.codec == media.CodecMSADPCM {
				if tt.channels == 1 {
					samplesPerBlock = (blockAlign-7)*2 + 2
				} else {
					samplesPerBlock = (blockAlign-14)*1 + 2
				}
			} else {
				samplesPerBlock = (blockAlign-4*tt.channels)*2/tt.channels + 1
			}

			// Seek to the start of the 5th block (block index 4)
			// Samples at the start of block 4: 4 * samplesPerBlock
			targetSample := int64(4 * samplesPerBlock)
			// duration = targetSample / sampleRate
			duration := time.Duration(targetSample) * time.Second / 8000

			if err := demuxer.Seek(duration); err != nil {
				t.Fatalf("Seek() error = %v", err)
			}

			// The next read packet should be exactly the 5th block of the original payload
			pkt, _, err := demuxer.ReadPacket()
			if err != nil {
				t.Fatalf("ReadPacket() error = %v", err)
			}

			expectedBlock := original[blockAlign*4 : blockAlign*5]
			if !bytes.Equal(pkt.Data(), expectedBlock) {
				t.Errorf("block mismatch: got %v, want %v", pkt.Data()[:10], expectedBlock[:10])
			}
		})
	}
}

func TestWAVMetadataRoundTrip(t *testing.T) {
	t.Parallel()
	originalAudio := []byte{0x10, 0x00, 0x20, 0x00, 0x30, 0x00, 0x40, 0x00}

	attr := media.MediaAttributes{
		Codec: media.CodecLPCM,
		Audio: media.AudioAttributes{
			SampleRate:    48000,
			Format:        media.SampleFormatS16,
			ChannelLayout: media.LayoutMono1,
		},
	}
	stream := media.StreamInfo{
		Index:           0,
		Type:            media.MediaAudio,
		IsDefault:       true,
		MediaAttributes: attr,
	}

	meta := metadata.NewBundle()
	meta.Set(metadata.KeyTitle("Test Odd"))
	meta.PushBack(metadata.KeyArtist("ArtistEven"))
	meta.Set(metadata.KeyGenre("Genre"))

	cuePayload := []byte{0x01, 0x02, 0x03}
	meta.AddRaw("cue ", cuePayload)

	smplPayload := []byte{0x01, 0x02, 0x03, 0x04}
	meta.AddRaw("smpl", smplPayload)

	out := testutil.NewBuffer(nil)
	muxer := NewMuxer(out, MuxerConfig{})
	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := muxer.SetMetadata(*meta); err != nil {
		t.Fatalf("SetMetadata() error = %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	pkt := media.NewPacket(len(originalAudio))
	copy(pkt.Data(), originalAudio)
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket() error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	demuxer, err := NewDemuxer(bytes.NewReader(out.Bytes()), DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	streams, parsedMeta, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if len(streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(streams))
	}

	if got := metadata.Get[metadata.KeyTitle](&parsedMeta); got != "Test Odd" {
		t.Errorf("Title = %q, want %q", got, "Test Odd")
	}
	artists := metadata.Enumerate[metadata.KeyArtist](&parsedMeta)
	if len(artists) != 1 || artists[0] != "ArtistEven" {
		t.Errorf("Artists = %v, want [%q]", artists, "ArtistEven")
	}
	if got := metadata.Get[metadata.KeyGenre](&parsedMeta); got != "Genre" {
		t.Errorf("Genre = %q, want %q", got, "Genre")
	}

	if rawCue, exists := parsedMeta.GetRaw("cue "); !exists || !bytes.Equal(rawCue[0], cuePayload) {
		t.Errorf("cue chunk = %v, want %v", rawCue, cuePayload)
	}

	if rawSmpl, exists := parsedMeta.GetRaw("smpl"); !exists || !bytes.Equal(rawSmpl[0], smplPayload) {
		t.Errorf("smpl chunk = %v, want %v", rawSmpl, smplPayload)
	}

	readPkt, _, err := demuxer.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket() error = %v", err)
	}
	if !bytes.Equal(readPkt.Data(), originalAudio) {
		t.Errorf("audio payload mismatch: got %v, want %v", readPkt.Data(), originalAudio)
	}
}

func TestWAVAnalyzeCompressedFormatTags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		audioFormat   uint16
		bitsPerSample uint16
		channels      uint16
		sampleRate    uint32
		blockAlign    uint16
		payload       []byte
		wantCodec     media.CodecID
	}{
		{
			name:          "MS ADPCM",
			audioFormat:   wavAudioMSADPCM,
			bitsPerSample: 4,
			channels:      1,
			sampleRate:    8000,
			blockAlign:    256,
			payload:       []byte{0x00, 0x01, 0x02, 0x03},
			wantCodec:     media.CodecMSADPCM,
		},
		{
			name:          "IMA ADPCM",
			audioFormat:   wavAudioIMAADPCM,
			bitsPerSample: 4,
			channels:      2,
			sampleRate:    22050,
			blockAlign:    512,
			payload:       []byte{0x10, 0x11, 0x12, 0x13},
			wantCodec:     media.CodecIMAADPCM,
		},
		{
			name:          "MP3",
			audioFormat:   wavAudioMP3,
			bitsPerSample: 0,
			channels:      2,
			sampleRate:    44100,
			blockAlign:    1,
			payload:       append([]byte{0xff, 0xfb, 0x90, 0x64}, make([]byte, 413)...),
			wantCodec:     media.CodecMP3,
		},
		{
			name:          "GSM",
			audioFormat:   wavAudioGSM,
			bitsPerSample: 0,
			channels:      1,
			sampleRate:    8000,
			blockAlign:    65,
			payload:       bytes.Repeat([]byte{0x55}, 65),
			wantCodec:     media.CodecGSM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wavData := buildWAVWithFormatTag(t, tt.audioFormat, tt.bitsPerSample, tt.channels, tt.sampleRate, tt.blockAlign, tt.payload)

			demuxer, err := NewDemuxer(bytes.NewReader(wavData), DemuxerConfig{})
			if err != nil {
				t.Fatalf("NewDemuxer() error = %v", err)
			}

			streams, _, err := demuxer.Analyze()
			if err != nil {
				t.Fatalf("Analyze() error = %v", err)
			}
			if len(streams) != 1 {
				t.Fatalf("Analyze() returned %d streams, want 1", len(streams))
			}

			stream := streams[0]
			if stream.MediaAttributes.Codec != tt.wantCodec {
				t.Fatalf("codec = %s, want %s", stream.MediaAttributes.Codec, tt.wantCodec)
			}
			if stream.MediaAttributes.Audio.Format != media.SampleFormatUnknown {
				t.Fatalf("sample format = %s, want unknown", stream.MediaAttributes.Audio.Format)
			}
			if stream.MediaAttributes.Audio.SampleRate != int(tt.sampleRate) {
				t.Fatalf("sample rate = %d, want %d", stream.MediaAttributes.Audio.SampleRate, tt.sampleRate)
			}
			if stream.MediaAttributes.Audio.ChannelLayout.ChannelCount() != int(tt.channels) {
				t.Fatalf("channels = %d, want %d", stream.MediaAttributes.Audio.ChannelLayout.ChannelCount(), tt.channels)
			}

			pkt, streamIndex, err := demuxer.ReadPacket()
			if err != nil {
				t.Fatalf("ReadPacket() error = %v", err)
			}
			if streamIndex != 0 {
				t.Fatalf("streamIndex = %d, want 0", streamIndex)
			}
			if !bytes.Equal(pkt.Data(), tt.payload) {
				t.Fatalf("packet data mismatch: got %v, want %v", pkt.Data(), tt.payload)
			}
		})
	}
}

func TestWAVMP3PacketizationDecodes(t *testing.T) {
	t.Parallel()
	mp3Data, err := os.ReadFile("../../codec-mp3/test/testdata/l3-sin1k0db.mp3")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	wavData := buildWAVWithFormatTag(t, wavAudioMP3, 0, 2, 44100, 1, mp3Data)
	demuxer, err := NewDemuxer(bytes.NewReader(wavData), DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	streams, _, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("Analyze() returned %d streams, want 1", len(streams))
	}
	if streams[0].MediaAttributes.Codec != media.CodecMP3 {
		t.Fatalf("codec = %s, want %s", streams[0].MediaAttributes.Codec, media.CodecMP3)
	}

	decoder := mp3codec.NewDecoderEngine(mp3codec.NewDecoderConfig())
	decodedFrames := 0
	decodedSamples := 0

	for i := 0; i < 4; i++ {
		pkt, _, err := demuxer.ReadPacket()
		if err != nil {
			t.Fatalf("ReadPacket() error = %v", err)
		}
		if len(pkt.Data()) == 0 {
			t.Fatal("ReadPacket() returned an empty packet")
		}
		if len(pkt.Data()) >= len(mp3Data) {
			t.Fatalf("packet length = %d, want less than full mp3 payload %d", len(pkt.Data()), len(mp3Data))
		}

		if err := decoder.SendPacket(pkt); err != nil {
			t.Fatalf("SendPacket() error = %v", err)
		}

		for {
			frame, err := decoder.ReceiveFrame()
			if err == engine.ErrEAGAIN {
				break
			}
			if err != nil {
				t.Fatalf("ReceiveFrame() error = %v", err)
			}

			audioFrame, ok := (*frame).(*media.AudioFrame)
			if !ok {
				t.Fatalf("decoded frame type = %T, want *media.AudioFrame", *frame)
			}
			decodedFrames++
			decodedSamples += audioFrame.Samples
		}
	}

	if decodedFrames == 0 || decodedSamples == 0 {
		t.Fatalf("decodedFrames = %d decodedSamples = %d, want non-zero", decodedFrames, decodedSamples)
	}
}

func TestWAVAnalyzeUnsupportedFormatTag(t *testing.T) {
	t.Parallel()
	wavData := buildWAVWithFormatTag(t, 0x1234, 8, 1, 8000, 1, []byte{0x00})
	demuxer, err := NewDemuxer(bytes.NewReader(wavData), DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	if _, _, err := demuxer.Analyze(); err == nil || err.Error() != "unsupported wav audio format tag: 4660" {
		t.Fatalf("Analyze() error = %v, want unsupported format tag", err)
	}
}

func buildWAVWithFormatTag(t *testing.T, audioFormat uint16, bitsPerSample uint16, channels uint16, sampleRate uint32, blockAlign uint16, payload []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	fmtSize := uint32(18)
	factSize := uint32(4)
	dataPad := uint32(len(payload) % 2)
	riffSize := uint32(4) + 8 + fmtSize + 8 + factSize + 8 + uint32(len(payload)) + dataPad
	byteRate := uint32(blockAlign) * sampleRate

	buf.WriteString(wavTagRIFF)
	binary.Write(&buf, binary.LittleEndian, riffSize)
	buf.WriteString(wavTagWAVE)

	buf.WriteString(wavTagFmt)
	binary.Write(&buf, binary.LittleEndian, fmtSize)
	binary.Write(&buf, binary.LittleEndian, audioFormat)
	binary.Write(&buf, binary.LittleEndian, channels)
	binary.Write(&buf, binary.LittleEndian, sampleRate)
	binary.Write(&buf, binary.LittleEndian, byteRate)
	binary.Write(&buf, binary.LittleEndian, blockAlign)
	binary.Write(&buf, binary.LittleEndian, bitsPerSample)
	binary.Write(&buf, binary.LittleEndian, uint16(0))

	buf.WriteString(wavTagFact)
	binary.Write(&buf, binary.LittleEndian, factSize)
	binary.Write(&buf, binary.LittleEndian, uint32(0))

	buf.WriteString(wavTagData)
	binary.Write(&buf, binary.LittleEndian, uint32(len(payload)))
	buf.Write(payload)
	if dataPad != 0 {
		buf.WriteByte(0)
	}

	return buf.Bytes()
}

func TestWAVADPCMRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		codec    media.CodecID
		channels int
	}{
		{"MS ADPCM Mono", media.CodecMSADPCM, 1},
		{"MS ADPCM Stereo", media.CodecMSADPCM, 2},
		{"IMA ADPCM Mono", media.CodecIMAADPCM, 1},
		{"IMA ADPCM Stereo", media.CodecIMAADPCM, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			blockAlign := 256 * tt.channels
			original := make([]byte, blockAlign*3)
			for i := range original {
				original[i] = byte(i % 256)
			}

			attr := media.MediaAttributes{
				Codec: tt.codec,
				Audio: media.AudioAttributes{
					SampleRate:    8000,
					Format:        media.SampleFormatUnknown,
					ChannelLayout: layoutFromChannelCount(tt.channels),
				},
			}

			wavData := buildTestWAVWithAttr(t, original, attr)

			demuxer, err := NewDemuxer(bytes.NewReader(wavData), DemuxerConfig{})
			if err != nil {
				t.Fatalf("NewDemuxer() error = %v", err)
			}

			streams, _, err := demuxer.Analyze()
			if err != nil {
				t.Fatalf("Analyze() error = %v", err)
			}
			if len(streams) != 1 {
				t.Fatalf("Analyze() returned %d streams, want 1", len(streams))
			}

			stream := streams[0]
			if stream.MediaAttributes.Codec != tt.codec {
				t.Errorf("codec = %s, want %s", stream.MediaAttributes.Codec, tt.codec)
			}
			if stream.MediaAttributes.Audio.Format != media.SampleFormatUnknown {
				t.Errorf("format = %s, want unknown", stream.MediaAttributes.Audio.Format)
			}

			var gotPayload []byte
			for {
				pkt, _, err := demuxer.ReadPacket()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("ReadPacket() error = %v", err)
				}
				gotPayload = append(gotPayload, pkt.Data()...)
			}

			if !bytes.Equal(gotPayload, original) {
				t.Errorf("payload mismatch: got %d bytes, want %d bytes", len(gotPayload), len(original))
			}
		})
	}
}

func TestWAVADPCMCodecParametersRoundTrip(t *testing.T) {
	t.Parallel()
	adpcm, err := params.Default(media.CodecMSADPCM, 2)
	if err != nil {
		t.Fatal(err)
	}
	adpcm.BlockAlign = 1024
	adpcm.SamplesPerBlock, err = params.SamplesPerBlock(media.CodecMSADPCM, 2, adpcm.BlockAlign)
	if err != nil {
		t.Fatal(err)
	}
	adpcm.Coefficients[0] = params.Coefficient{Coeff1: 128, Coeff2: 64}

	attr := media.MediaAttributes{
		Codec:           media.CodecMSADPCM,
		CodecParameters: media.NewCodecParameters[params.ADPCM](adpcm.MarshalBinary()),
		Audio: media.AudioAttributes{
			SampleRate:    8000,
			ChannelLayout: media.LayoutStereo2_0,
		},
	}
	payload := make([]byte, int(adpcm.BlockAlign)*2)
	wavData := buildTestWAVWithAttr(t, payload, attr)

	demuxer, err := NewDemuxer(bytes.NewReader(wavData), DemuxerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	streams, _, err := demuxer.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	got, err := params.Parse(media.CodecMSADPCM, 2, streams[0].CodecParameters.Data)
	if err != nil {
		t.Fatal(err)
	}
	if !media.IsCodecParameters[params.ADPCM](streams[0].CodecParameters) || got.BlockAlign != adpcm.BlockAlign || got.SamplesPerBlock != adpcm.SamplesPerBlock || got.Coefficients[0] != adpcm.Coefficients[0] {
		t.Fatalf("ADPCM parameters were not preserved: %#v", got)
	}

	for i := 0; i < 2; i++ {
		pkt, _, err := demuxer.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if len(pkt.Data()) != int(adpcm.BlockAlign) {
			t.Fatalf("packet %d size = %d, want %d", i, len(pkt.Data()), adpcm.BlockAlign)
		}
	}
}
