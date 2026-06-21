package internal

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"testing"

	"github.com/godexture/core/domain/media"
)

type seekableBuffer struct {
	buf []byte
	off int64
}

func (s *seekableBuffer) Write(p []byte) (n int, err error) {
	end := s.off + int64(len(p))
	if end > int64(len(s.buf)) {
		newBuf := make([]byte, end)
		copy(newBuf, s.buf)
		s.buf = newBuf
	}
	copy(s.buf[s.off:], p)
	s.off = end
	return len(p), nil
}

func (s *seekableBuffer) Seek(offset int64, whence int) (int64, error) {
	var newOff int64
	switch whence {
	case io.SeekStart:
		newOff = offset
	case io.SeekCurrent:
		newOff = s.off + offset
	case io.SeekEnd:
		newOff = int64(len(s.buf)) + offset
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}
	if newOff < 0 {
		return 0, fmt.Errorf("negative position: %d", newOff)
	}
	s.off = newOff
	return newOff, nil
}

func TestProbercognizesWAVSignature(t *testing.T) {
	data := buildTestWAV(t, []byte{0x01, 0x02, 0x03, 0x04})

	if got := Probe(bytes.NewReader(data)); got != 100 {
		t.Fatalf("Probe() = %d, want %d", got, 100)
	}
}

func TestWAVRoundTripMonoPCM16(t *testing.T) {
	original := []byte{0x10, 0x00, 0x20, 0x00, 0x30, 0x00, 0x40, 0x00}
	wavData := buildTestWAV(t, original)

	demuxer, err := NewDemuxer(bytes.NewReader(wavData))
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

	var out seekableBuffer
	muxer := NewMuxer(&out)
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

	if !bytes.Equal(out.buf, wavData) {
		t.Fatalf("muxed wav mismatch: got %d bytes, want %d bytes", len(out.buf), len(wavData))
	}
}

func TestWAVRoundTripPCM24(t *testing.T) {
	// 24-bit PCM (3 bytes per sample). Let's do 2 channels. 3 samples each = 18 bytes.
	original := []byte{
		0x01, 0x02, 0x03,  0x04, 0x05, 0x06, // sample 1 (L, R)
		0x07, 0x08, 0x09,  0x0a, 0x0b, 0x0c, // sample 2 (L, R)
		0x0d, 0x0e, 0x0f,  0x10, 0x11, 0x12, // sample 3 (L, R)
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

	demuxer, err := NewDemuxer(bytes.NewReader(wavData))
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

	var out seekableBuffer
	muxer := NewMuxer(&out)
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

	if !bytes.Equal(out.buf, wavData) {
		t.Fatalf("muxed wav mismatch: got %d bytes, want %d bytes", len(out.buf), len(wavData))
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

	var out seekableBuffer
	muxer := NewMuxer(&out)
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

	return out.buf
}

func TestRF64RoundTrip(t *testing.T) {
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
	headerBytes, err := buildWAVHeader(attr, fakeSize, false)
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
	demuxer, err := NewDemuxer(bytes.NewReader(headerBytes))
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
	headerBytes, err := buildWAVHeader(attr, fakeSize, false)
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
	// 1 channel, Float32. This should trigger writeFact (since F32 is non-PCM).
	original := []byte{
		0x00, 0x00, 0x80, 0x3f, // 1.0f
		0x00, 0x00, 0x00, 0x40, // 2.0f
	}

	var out seekableBuffer
	muxer := NewMuxer(&out)
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

	wavData := out.buf

	// Verify that the wavData contains "fact" chunk.
	if !bytes.Contains(wavData, []byte("fact")) {
		t.Errorf("expected WAV header to contain 'fact' chunk for Float32, but it did not")
	}

	// Now demux it and verify
	demuxer, err := NewDemuxer(bytes.NewReader(wavData))
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
	// Construct a minimal RF64 WAV file manually.
	var buf bytes.Buffer
	buf.WriteString("RF64")
	binary.Write(&buf, binary.LittleEndian, uint32(0xFFFFFFFF))
	buf.WriteString("WAVE")

	// ds64
	buf.WriteString("ds64")
	binary.Write(&buf, binary.LittleEndian, uint32(28))
	binary.Write(&buf, binary.LittleEndian, uint64(1000))      // riffSize
	binary.Write(&buf, binary.LittleEndian, uint64(100))       // dataSize
	binary.Write(&buf, binary.LittleEndian, uint64(50))        // numSamples
	binary.Write(&buf, binary.LittleEndian, uint32(0))         // tableLength

	// fmt
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, uint16(1))         // format PCM
	binary.Write(&buf, binary.LittleEndian, uint16(1))         // channels
	binary.Write(&buf, binary.LittleEndian, uint32(48000))     // sampleRate
	binary.Write(&buf, binary.LittleEndian, uint32(96000))     // byteRate
	binary.Write(&buf, binary.LittleEndian, uint16(2))         // blockAlign
	binary.Write(&buf, binary.LittleEndian, uint16(16))        // bitsPerSample

	// data
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(0xFFFFFFFF))
	buf.Write(make([]byte, 100))

	demuxer, err := NewDemuxer(bytes.NewReader(buf.Bytes()))
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

	demuxer, err := NewDemuxer(bytes.NewReader(wavData))
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

	demuxer, err := NewDemuxer(bytes.NewReader(wavData))
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
	var out bytes.Buffer
	muxer := NewMuxer(&out)
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

func TestWAVMuxerForceRF64(t *testing.T) {
	var out seekableBuffer
	muxer := NewMuxer(&out)
	muxer.ForceRF64 = true
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

	wavData := out.buf
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


