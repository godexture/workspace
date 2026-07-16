package internal

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/format-flac/frame"
	"github.com/godexture/format-flac/streaminfo"
	vc "github.com/godexture/metadata-vorbiscomment"
	"github.com/godexture/sdk/hash"
)

const appendixDExample1Hex = "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a"

func TestProbe(t *testing.T) {
	if got := Probe(bytes.NewReader(mustDecodeHex(t, appendixDExample1Hex))); got != manifest.ProbeExactSignature {
		t.Fatalf("Probe() = %d, want %d", got, manifest.ProbeExactSignature)
	}
	if got := Probe(bytes.NewReader([]byte("nope"))); got != manifest.ProbeMismatch {
		t.Fatalf("Probe(non-flac) = %d, want %d", got, manifest.ProbeMismatch)
	}
}

func BenchmarkDemuxerReadPackets(b *testing.B) {
	audio := make([]byte, 4<<20)
	input := makeTestFLAC(b, 256<<10, audio)
	b.ReportAllocs()
	b.SetBytes(int64(len(audio)))
	b.ResetTimer()
	for b.Loop() {
		demuxer, err := NewDemuxer(bytes.NewReader(input))
		if err != nil {
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

func TestDemuxerAnalyzeAndReadPacket(t *testing.T) {
	data := mustDecodeHex(t, appendixDExample1Hex)
	demuxer, err := NewDemuxer(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	streams, meta, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if meta.AllRaw() != nil {
		t.Fatalf("expected no global raw metadata, got %v", meta.AllRaw())
	}
	if len(streams) != 1 {
		t.Fatalf("Analyze() returned %d streams, want 1", len(streams))
	}

	stream := streams[0]
	if stream.Type != media.MediaAudio {
		t.Fatalf("stream type = %s, want %s", stream.Type, media.MediaAudio)
	}
	if stream.Codec != media.CodecFLAC {
		t.Fatalf("codec = %s, want %s", stream.Codec, media.CodecFLAC)
	}
	if stream.Audio.SampleRate != 44100 {
		t.Fatalf("sample rate = %d, want 44100", stream.Audio.SampleRate)
	}
	if stream.Audio.ChannelCount() != 2 {
		t.Fatalf("channels = %d, want 2", stream.Audio.ChannelCount())
	}
	if stream.Audio.Format != media.SampleFormatS16 {
		t.Fatalf("format = %s, want %s", stream.Audio.Format, media.SampleFormatS16)
	}
	if raw, ok := stream.Metadata.GetRaw(streaminfo.MetadataKey); !ok || len(raw) != 1 || len(raw[0]) != streaminfo.Length {
		t.Fatalf("missing STREAMINFO raw metadata: ok=%v len=%d", ok, len(raw))
	}

	pkt, idx, err := demuxer.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket() error = %v", err)
	}
	if idx != 0 || pkt.StreamIndex != 0 {
		t.Fatalf("stream index = (%d, %d), want 0", idx, pkt.StreamIndex)
	}
	if pkt.MediaType != media.MediaAudio {
		t.Fatalf("packet media type = %s, want %s", pkt.MediaType, media.MediaAudio)
	}
	wantFrameBytes := data[42:]
	if !bytes.Equal(pkt.Data(), wantFrameBytes) {
		t.Fatalf("packet data = % x, want % x", pkt.Data(), wantFrameBytes)
	}

	pkt.Release()
	pkt, _, err = demuxer.ReadPacket()
	if err != io.EOF || pkt != nil {
		t.Fatalf("second ReadPacket() = pkt %v err %v, want EOF", pkt, err)
	}
}

func TestDemuxerReadPacketStreamsAudioInChunks(t *testing.T) {
	example := mustDecodeHex(t, appendixDExample1Hex)
	info, err := streaminfo.Parse(example[8:42])
	if err != nil {
		t.Fatal(err)
	}
	first := append([]byte(nil), example[42:]...)
	second := append([]byte(nil), first...)
	header, err := frame.ParseHeader(second, info)
	if err != nil {
		t.Fatal(err)
	}
	second[4] = 1
	second[header.HeaderBytes-1] = hash.CRC8(second[:header.HeaderBytes-1])
	crc := hash.CRC16(second[:len(second)-2])
	second[len(second)-2], second[len(second)-1] = byte(crc>>8), byte(crc)
	demuxer, err := NewDemuxer(bytes.NewReader(makeTestFLAC(t, 0, append(first, second...))))
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}

	var packets []*media.Packet
	for {
		packet, _, err := demuxer.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ReadPacket() error = %v", err)
		}
		packets = append(packets, packet)
	}
	defer func() {
		for _, packet := range packets {
			packet.Release()
		}
	}()
	if len(packets) != 2 || !bytes.Equal(packets[0].Data(), first) || !bytes.Equal(packets[1].Data(), second) {
		t.Fatal("demuxer did not return one packet per FLAC frame")
	}
	if packets[0].PTS != 0 || packets[1].PTS != 1 || packets[0].DTS != 0 || packets[1].DTS != 1 {
		t.Fatalf("packet timestamps = (%d, %d), (%d, %d)", packets[0].PTS, packets[0].DTS, packets[1].PTS, packets[1].DTS)
	}
}

func TestDemuxerEmptyAudio(t *testing.T) {
	demuxer, err := NewDemuxer(bytes.NewReader(makeTestFLAC(t, 0, nil)))
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}
	packet, _, err := demuxer.ReadPacket()
	if err != io.EOF || packet != nil {
		t.Fatalf("ReadPacket() = (%v, %v), want (nil, EOF)", packet, err)
	}
}

func TestDemuxerReadErrorReturnsNoPartialPacket(t *testing.T) {
	input := makeTestFLAC(t, 0, nil)
	wantErr := errors.New("injected read failure")
	reader := &failingReadSeeker{
		Reader:    bytes.NewReader(input),
		failAfter: len(input),
		err:       wantErr,
	}
	demuxer, err := NewDemuxer(reader)
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}
	packet, _, err := demuxer.ReadPacket()
	if packet != nil || !errors.Is(err, wantErr) {
		t.Fatalf("ReadPacket() = (%v, %v), want (nil, injected error)", packet, err)
	}
}

func TestLargeMetadataRoundtripPreservesOpaqueBlocks(t *testing.T) {
	large := make([]byte, 2*(64<<10)+17)
	for i := range large {
		large[i] = byte(i * 31)
	}
	input := makeTestFLAC(t, 0, nil, large, []byte("second block"))
	demuxer, err := NewDemuxer(bytes.NewReader(input))
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}
	streams, meta, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	var output bytes.Buffer
	muxer := NewMuxer(&output)
	if _, err := muxer.AddStream(streams[0]); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := muxer.SetMetadata(meta); err != nil {
		t.Fatalf("SetMetadata() error = %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	roundtrip, err := NewDemuxer(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatalf("NewDemuxer(roundtrip) error = %v", err)
	}
	gotStreams, gotMeta, err := roundtrip.Analyze()
	if err != nil {
		t.Fatalf("Analyze(roundtrip) error = %v", err)
	}
	_ = gotStreams
	want, _ := meta.GetRaw(streaminfo.MetadataBlockKey)
	got, _ := gotMeta.GetRaw(streaminfo.MetadataBlockKey)
	if len(got) != len(want) {
		t.Fatalf("metadata block count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("metadata block %d changed during roundtrip", i)
		}
	}
}

func TestDemuxerDecodesVorbisCommentAndPicture(t *testing.T) {
	commentBundle := metadata.NewBundle()
	commentBundle.Set(metadata.KeyTitle("Song"))
	commentBundle.PushBack(metadata.KeyArtist("First"))
	commentBundle.PushBack(metadata.KeyArtist("Second"))
	comments := vc.Marshal(*commentBundle)
	picture := vc.MarshalPicture(metadata.Thumbnail{
		Data:        []byte{1, 2, 3},
		MIMEType:    "image/png",
		PictureType: metadata.PictureTypeFrontCover,
	})
	input := makeTestFLACWithBlocks(t, nil, []metadataBlock{
		{blockType: streaminfo.MetadataTypeVorbisComment, payload: comments},
		{blockType: streaminfo.MetadataTypePicture, payload: picture},
	})

	demuxer, err := NewDemuxer(bytes.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	_, bundle, err := demuxer.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	if got := metadata.Get[metadata.KeyTitle](&bundle); got != "Song" {
		t.Fatalf("title = %q", got)
	}
	if got := metadata.Enumerate[metadata.KeyArtist](&bundle); len(got) != 2 || got[0] != "First" || got[1] != "Second" {
		t.Fatalf("artists = %#v", got)
	}
	if got := metadata.Get[metadata.KeyThumbnail](&bundle); len(got) != 1 || !bytes.Equal(got[0].Data, []byte{1, 2, 3}) {
		t.Fatalf("thumbnails = %#v", got)
	}
}

func makeTestFLAC(t testing.TB, maxFrameSize uint32, audio []byte, extraPayloads ...[]byte) []byte {
	t.Helper()
	example := mustDecodeHex(t, appendixDExample1Hex)
	streamInfo := append([]byte(nil), example[8:42]...)
	streamInfo[7] = byte(maxFrameSize >> 16)
	streamInfo[8] = byte(maxFrameSize >> 8)
	streamInfo[9] = byte(maxFrameSize)

	data := append([]byte(nil), []byte(streaminfo.Marker)...)
	streamInfoType := byte(streaminfo.MetadataTypeStreamInfo)
	if len(extraPayloads) == 0 {
		streamInfoType |= 0x80
	}
	data = appendTestMetadataBlock(data, streamInfoType, streamInfo)
	for i, payload := range extraPayloads {
		blockType := byte(2 + i)
		if i == len(extraPayloads)-1 {
			blockType |= 0x80
		}
		data = appendTestMetadataBlock(data, blockType, payload)
	}
	return append(data, audio...)
}

func makeTestFLACWithBlocks(t testing.TB, audio []byte, blocks []metadataBlock) []byte {
	t.Helper()
	example := mustDecodeHex(t, appendixDExample1Hex)
	streamInfo := append([]byte(nil), example[8:42]...)
	data := append([]byte(nil), []byte(streaminfo.Marker)...)
	streamInfoType := byte(streaminfo.MetadataTypeStreamInfo)
	if len(blocks) == 0 {
		streamInfoType |= 0x80
	}
	data = appendTestMetadataBlock(data, streamInfoType, streamInfo)
	for i, block := range blocks {
		blockType := block.blockType
		if i == len(blocks)-1 {
			blockType |= 0x80
		}
		data = appendTestMetadataBlock(data, blockType, block.payload)
	}
	return append(data, audio...)
}

func appendTestMetadataBlock(dst []byte, blockType byte, payload []byte) []byte {
	length := len(payload)
	dst = append(dst, blockType, byte(length>>16), byte(length>>8), byte(length))
	return append(dst, payload...)
}

type failingReadSeeker struct {
	*bytes.Reader
	read      int
	failAfter int
	err       error
}

func (r *failingReadSeeker) Read(data []byte) (int, error) {
	if r.read >= r.failAfter {
		return 0, r.err
	}
	if remaining := r.failAfter - r.read; len(data) > remaining {
		data = data[:remaining]
	}
	n, err := r.Reader.Read(data)
	r.read += n
	return n, err
}

func mustDecodeHex(t testing.TB, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("hex.DecodeString() error = %v", err)
	}
	return data
}
