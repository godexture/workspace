package internal_test

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"testing"
	"time"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/format-mp3/internal"
)

func TestProbe_ValidMP3(t *testing.T) {
	t.Parallel()
	// 0xFF 0xFB = MPEG1 Layer3 の有効な同期ワード
	mp3Data := []byte{0xFF, 0xFB, 0x90, 0x00}
	score := internal.Probe(bytes.NewReader(mp3Data))
	if score < manifest.ProbeSingleSync {
		t.Errorf("expected ProbeSingleSync or higher, got %d", score)
	}
}

func TestProbe_ID3Header(t *testing.T) {
	t.Parallel()
	id3Data := []byte{'I', 'D', '3', 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	score := internal.Probe(bytes.NewReader(id3Data))
	if score < manifest.ProbeSharedMetadata {
		t.Errorf("expected ProbeSharedMetadata or higher, got %d", score)
	}
}

func TestProbe_NotMP3(t *testing.T) {
	t.Parallel()
	otherData := []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00}
	score := internal.Probe(bytes.NewReader(otherData))
	if score != manifest.ProbeMismatch {
		t.Errorf("expected ProbeMismatch, got %d", score)
	}
}

func TestDemuxerAnalyzeRejectsNonMP3(t *testing.T) {
	t.Parallel()
	demuxer, err := internal.NewDemuxer(bytes.NewReader([]byte("RIFF\x00\x00\x00\x00WAVE")), internal.DemuxerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := demuxer.Analyze(); err == nil || err.Error() != "not a mp3 stream" {
		t.Fatalf("Analyze() error = %v, want not a mp3 stream", err)
	}
}

func TestSkipID3v2(t *testing.T) {
	t.Parallel()
	tag := append([]byte{'I', 'D', '3', 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03}, []byte("abc")...)
	mp3Frame := []byte{0xFF, 0xFB, 0x90, 0x00}
	br := bufio.NewReader(bytes.NewReader(append(tag, mp3Frame...)))

	skipped, err := internal.SkipID3v2(br)
	if err != nil {
		t.Fatalf("SkipID3v2 returned error: %v", err)
	}
	if skipped != len(tag) {
		t.Fatalf("skipped = %d, want %d", skipped, len(tag))
	}

	got, err := br.Peek(len(mp3Frame))
	if err != nil {
		t.Fatalf("Peek returned error: %v", err)
	}
	if !bytes.Equal(got, mp3Frame) {
		t.Fatalf("Peek = %v, want %v", got, mp3Frame)
	}
}

func TestDemuxerAnalyze_ParsesID3Metadata(t *testing.T) {
	t.Parallel()
	audio, err := os.ReadFile("../../codec-mp3/test/testdata/l3-sin1k0db.mp3")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	titleFrame := append([]byte("TIT2"), []byte{0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x03, 'T', 'e', 's', 't'}...)
	artistFrame := append([]byte("TPE1"), []byte{0x00, 0x00, 0x00, 0x07, 0x00, 0x00, 0x03, 'A', 'r', 't', 'i', 's', 't'}...)
	tagPayload := append(titleFrame, artistFrame...)
	id3Header := []byte{'I', 'D', '3', 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, byte(len(tagPayload))}
	file := append(append(id3Header, tagPayload...), audio...)

	demuxer, err := internal.NewDemuxer(bytes.NewReader(file), internal.DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer returned error: %v", err)
	}

	streams, bundle, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("len(streams) = %d, want 1", len(streams))
	}

	metadata.AssertBundleValue(t, &bundle, metadata.KeyTitle("Test"))
	metadata.AssertBundleSlice(t, &bundle, []metadata.KeyArtist{"Artist"})
	metadata.AssertBundleValue(t, &streams[0].Metadata, metadata.KeyTitle("Test"))
}

func BenchmarkDemuxerReadPackets(b *testing.B) {
	audio, err := os.ReadFile("../../codec-mp3/test/testdata/l3-sin1k0db.mp3")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(audio)))
	b.ResetTimer()
	for b.Loop() {
		demuxer, err := internal.NewDemuxer(bytes.NewReader(audio), internal.DemuxerConfig{})
		if err != nil {
			b.Fatal(err)
		}
		if _, _, err := demuxer.Analyze(); err != nil {
			b.Fatal(err)
		}
		for {
			packet, _, err := demuxer.ReadPacket()
			if err == io.EOF {
				break
			}
			if err != nil {
				b.Fatal(err)
			}
			packet.Release()
		}
	}
}

func TestDemuxerResynchronizesAfterCorruptBytes(t *testing.T) {
	t.Parallel()
	audio := append(syntheticMP3Frame(1), syntheticMP3Frame(2)...)
	audio = append(audio, 0x00, 0x12, 0x34)
	audio = append(audio, syntheticMP3Frame(3)...)
	audio = append(audio, syntheticMP3Frame(4)...)

	demuxer, err := internal.NewDemuxer(bytes.NewReader(audio), internal.DemuxerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := demuxer.Analyze(); err != nil {
		t.Fatal(err)
	}
	for want := byte(1); want <= 4; want++ {
		packet, _, err := demuxer.ReadPacket()
		if err != nil {
			t.Fatalf("ReadPacket %d: %v", want, err)
		}
		if got := packet.Data()[4]; got != want {
			packet.Release()
			t.Fatalf("packet marker = %d, want %d", got, want)
		}
		packet.Release()
	}
	if packet, _, err := demuxer.ReadPacket(); err != io.EOF || packet != nil {
		t.Fatalf("final ReadPacket = (%v, %v), want nil, EOF", packet, err)
	}
}

func TestDemuxerIgnoresTruncatedTrailingFrame(t *testing.T) {
	t.Parallel()
	audio := append(syntheticMP3Frame(1), syntheticMP3Frame(2)...)
	audio = append(audio, syntheticMP3Frame(3)[:100]...)

	demuxer, err := internal.NewDemuxer(bytes.NewReader(audio), internal.DemuxerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := demuxer.Analyze(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		packet, _, err := demuxer.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		packet.Release()
	}
	if packet, _, err := demuxer.ReadPacket(); err != io.EOF || packet != nil {
		t.Fatalf("truncated ReadPacket = (%v, %v), want nil, EOF", packet, err)
	}
}

func syntheticMP3Frame(marker byte) []byte {
	const frameSize = 417
	frame := make([]byte, frameSize)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x00})
	for i := 4; i < len(frame); i++ {
		frame[i] = marker
	}
	return frame
}

func TestMuxer_WritesID3Metadata(t *testing.T) {
	t.Parallel()
	audio, err := os.ReadFile("../../codec-mp3/test/testdata/l3-sin1k0db.mp3")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var out bytes.Buffer
	muxer := internal.NewMuxer(&out, internal.MuxerConfig{})
	stream := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecMP3,
			Audio: media.AudioAttributes{
				SampleRate:    44100,
				Format:        media.SampleFormatS16,
				ChannelLayout: media.LayoutStereo2_0,
			},
		},
	}
	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream returned error: %v", err)
	}

	meta := *metadata.NewBundle()
	meta.Set(metadata.KeyTitle("Written Title"))
	meta.PushBack(metadata.KeyArtist("Written Artist"))
	if err := muxer.SetMetadata(meta); err != nil {
		t.Fatalf("SetMetadata returned error: %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader returned error: %v", err)
	}

	pkt := media.NewPacket(len(audio))
	copy(pkt.Data(), audio)
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket returned error: %v", err)
	}

	demuxer, err := internal.NewDemuxer(bytes.NewReader(out.Bytes()), internal.DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer returned error: %v", err)
	}
	_, bundle, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	metadata.AssertBundleValue(t, &bundle, metadata.KeyTitle("Written Title"))
	metadata.AssertBundleSlice(t, &bundle, []metadata.KeyArtist{"Written Artist"})
}

func TestMuxer_ImplicitHeaderWriting(t *testing.T) {
	t.Parallel()
	audio, err := os.ReadFile("../../codec-mp3/test/testdata/l3-sin1k0db.mp3")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var out bytes.Buffer
	muxer := internal.NewMuxer(&out, internal.MuxerConfig{})
	stream := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecMP3,
			Audio: media.AudioAttributes{
				SampleRate:    44100,
				Format:        media.SampleFormatS16,
				ChannelLayout: media.LayoutStereo2_0,
			},
		},
	}
	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatalf("AddStream returned error: %v", err)
	}

	meta := *metadata.NewBundle()
	meta.Set(metadata.KeyTitle("Implicit Title"))
	meta.PushBack(metadata.KeyArtist("Implicit Artist"))
	if err := muxer.SetMetadata(meta); err != nil {
		t.Fatalf("SetMetadata returned error: %v", err)
	}
	// WriteHeader explicitly omitted to test implicit write during WritePacket

	pkt := media.NewPacket(len(audio))
	copy(pkt.Data(), audio)
	if err := muxer.WritePacket(0, pkt); err != nil {
		t.Fatalf("WritePacket returned error: %v", err)
	}

	demuxer, err := internal.NewDemuxer(bytes.NewReader(out.Bytes()), internal.DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer returned error: %v", err)
	}
	_, bundle, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	metadata.AssertBundleValue(t, &bundle, metadata.KeyTitle("Implicit Title"))
	metadata.AssertBundleSlice(t, &bundle, []metadata.KeyArtist{"Implicit Artist"})
}

func TestDemuxerSeek_CBR(t *testing.T) {
	t.Parallel()
	audio, err := os.ReadFile("../../codec-mp3/test/testdata/l3-sin1k0db.mp3")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	demuxer, err := internal.NewDemuxer(bytes.NewReader(audio), internal.DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer returned error: %v", err)
	}

	_, _, err = demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	// Seek to 0.5 seconds
	if err := demuxer.Seek(500 * time.Millisecond); err != nil {
		t.Fatalf("Seek returned error: %v", err)
	}

	pkt, _, err := demuxer.ReadPacket()
	if err != nil && err != io.EOF {
		t.Fatalf("ReadPacket returned error: %v", err)
	}

	if pkt == nil || len(pkt.Data()) == 0 {
		t.Fatalf("expected a valid packet after seek")
	}

	// Make sure the PTS is somewhat correct
	if pkt.PTS <= 0 {
		t.Errorf("expected PTS to be > 0 after seek to 0.5s, got %d", pkt.PTS)
	}
}

type trackingReader struct {
	*bytes.Reader
	lastSeekOffset int64
}

func (tr *trackingReader) Seek(offset int64, whence int) (int64, error) {
	off, err := tr.Reader.Seek(offset, whence)
	if err == nil && whence == io.SeekStart {
		tr.lastSeekOffset = off
	}
	return off, err
}

func TestDemuxerSeek_Xing(t *testing.T) {
	t.Parallel()
	// Frame size = 417 bytes.
	// MPEG1 Layer 3, Stereo, 128 kbps, 44100 Hz.
	frame1 := make([]byte, 417)
	copy(frame1[0:4], []byte{0xFF, 0xFB, 0x90, 0x00})
	copy(frame1[36:40], []byte("Xing"))
	// Flags (Frames | Bytes | TOC)
	binary.BigEndian.PutUint32(frame1[40:44], 7)
	// Frames = 100
	binary.BigEndian.PutUint32(frame1[44:48], 100)
	// Bytes = 41700
	binary.BigEndian.PutUint32(frame1[48:52], 41700)
	// TOC
	for i := 0; i < 100; i++ {
		frame1[52+i] = byte(i * 255 / 99)
	}

	frame2Header := []byte{0xFF, 0xFB, 0x90, 0x00}

	// Create payload
	payload := append(frame1, frame2Header...)

	tr := &trackingReader{Reader: bytes.NewReader(payload)}
	demuxer, err := internal.NewDemuxer(tr, internal.DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer returned error: %v", err)
	}

	_, _, err = demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	// Xing duration = 100 frames * 1152 samples / 44100 = 2.6122 seconds
	// Let's seek to 50% of duration
	duration := time.Duration(100*1152) * time.Second / 44100
	seekTime := duration/2 + 1*time.Nanosecond
	if err := demuxer.Seek(seekTime); err != nil {
		t.Fatalf("Seek returned error: %v", err)
	}

	// 50% TOC index is 50. TOC[50] = 50 * 255 / 99 = 128.
	// Expected offset = (128 / 256) * 41700 = 20850.
	// targetOffset = firstFrameOffset (0) + 20850 = 20850.
	expectedOffset := int64(20850)
	if tr.lastSeekOffset != expectedOffset {
		t.Errorf("expected seek offset %d, got %d", expectedOffset, tr.lastSeekOffset)
	}
}

func TestDemuxerSeek_VBRI(t *testing.T) {
	t.Parallel()
	// Frame size = 417 bytes.
	// MPEG1 Layer 3, Stereo, 128 kbps, 44100 Hz.
	frame1 := make([]byte, 417)
	copy(frame1[0:4], []byte{0xFF, 0xFB, 0x90, 0x00})
	copy(frame1[36:40], []byte("VBRI"))
	binary.BigEndian.PutUint16(frame1[40:42], 1)     // version
	binary.BigEndian.PutUint16(frame1[42:44], 0)     // delay
	binary.BigEndian.PutUint16(frame1[44:46], 0)     // quality
	binary.BigEndian.PutUint32(frame1[46:50], 41700) // bytes
	binary.BigEndian.PutUint32(frame1[50:54], 100)   // frames
	binary.BigEndian.PutUint16(frame1[54:56], 10)    // TOC entries
	binary.BigEndian.PutUint16(frame1[56:58], 1)     // scale
	binary.BigEndian.PutUint16(frame1[58:60], 2)     // entry size
	binary.BigEndian.PutUint16(frame1[60:62], 10)    // frames per entry
	for i := 0; i < 10; i++ {
		// Each entry size is 4170 bytes
		binary.BigEndian.PutUint16(frame1[62+i*2:64+i*2], 4170)
	}

	frame2Header := []byte{0xFF, 0xFB, 0x90, 0x00}

	payload := append(frame1, frame2Header...)

	tr := &trackingReader{Reader: bytes.NewReader(payload)}
	demuxer, err := internal.NewDemuxer(tr, internal.DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer returned error: %v", err)
	}

	_, _, err = demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	// VBRI duration = 100 frames * 1152 samples / 44100 = 2.6122 seconds
	// durationPerEntry = 10 frames * 1152 samples / 44100 = 0.26122 seconds
	// Seek to 50% of duration (5.0 entries)
	duration := time.Duration(100*1152) * time.Second / 44100
	seekTime := duration/2 + 1*time.Nanosecond
	if err := demuxer.Seek(seekTime); err != nil {
		t.Fatalf("Seek returned error: %v", err)
	}

	// 5.0 entries means entryIndex = 5, fraction = 0.0
	// startOffset = TOC[4] = 4170 * 5 = 20850. (Wait, TOC[0]=4170, TOC[1]=8340, TOC[2]=12510, TOC[3]=16680, TOC[4]=20850).
	// endOffset = TOC[5] = 25020.
	// Since fraction = 0, expected offset = 20850.
	expectedOffset := int64(20850)
	if tr.lastSeekOffset != expectedOffset {
		t.Errorf("expected seek offset %d, got %d", expectedOffset, tr.lastSeekOffset)
	}
}
