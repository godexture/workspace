package internal

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/plugin/flac/internal/streaminfo"
	"github.com/godexture/godec/sdk/hash"
)

func TestMuxerWritesMeasuredStreamInfoToSeekableOutput(t *testing.T) {
	t.Parallel()
	file, err := os.CreateTemp(t.TempDir(), "stream-*.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	muxer := NewMuxer(file, MuxerConfig{})
	addTestStream(t, muxer)
	writeTestFrame(t, muxer, testFrame(4096, 0, 100))
	writeTestFrame(t, muxer, testFrame(4096, 1, 120))
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	info := readTestStreamInfo(t, file)
	if info.MinBlockSize != 4096 || info.MaxBlockSize != 4096 {
		t.Fatalf("block sizes = (%d, %d), want (4096, 4096)", info.MinBlockSize, info.MaxBlockSize)
	}
	if info.TotalSamples != 8192 {
		t.Fatalf("TotalSamples = %d, want 8192", info.TotalSamples)
	}
	if info.MinFrameSize != 100 || info.MaxFrameSize != 120 {
		t.Fatalf("frame sizes = (%d, %d), want (100, 120)", info.MinFrameSize, info.MaxFrameSize)
	}
	if info.MD5 != [16]byte{} {
		t.Fatalf("MD5 = %x, want unset", info.MD5)
	}
}

func TestMuxerAppliesPCMMD5EndEvent(t *testing.T) {
	t.Parallel()
	file, err := os.CreateTemp(t.TempDir(), "stream-*.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	muxer := NewMuxer(file, MuxerConfig{})
	addTestStream(t, muxer)
	writeTestFrame(t, muxer, testFrame(4096, 0, 100))
	want := [16]byte{1, 2, 3, 4}
	event := media.NewPacketEvent(media.PacketKindStreamEnd, 0, []media.CodecParameters{
		media.NewCodecParameters[streaminfo.PCMMD5Parameters](want[:]),
	})
	defer event.Release()
	if err := muxer.WritePacket(0, event); err != nil {
		t.Fatalf("WritePacket(end event) error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}
	if got := readTestStreamInfo(t, file).MD5; got != want {
		t.Fatalf("MD5 = %x, want %x", got, want)
	}
}

func TestMuxerWritesEmptyStreamPCMMD5(t *testing.T) {
	t.Parallel()
	file, err := os.CreateTemp(t.TempDir(), "stream-*.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	muxer := NewMuxer(file, MuxerConfig{})
	addTestStream(t, muxer)
	want := [16]byte{5, 6, 7, 8}
	event := media.NewPacketEvent(media.PacketKindStreamEnd, 0, []media.CodecParameters{
		media.NewCodecParameters[streaminfo.PCMMD5Parameters](want[:]),
	})
	defer event.Release()
	if err := muxer.WritePacket(0, event); err != nil {
		t.Fatalf("WritePacket(end event) error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}
	if got := readTestStreamInfo(t, file).MD5; got != want {
		t.Fatalf("MD5 = %x, want %x", got, want)
	}
}

func TestMuxerKeepsInitialStreamInfoForNonSeekableOutput(t *testing.T) {
	t.Parallel()
	var output nonSeekBuffer
	muxer := NewMuxer(&output, MuxerConfig{})
	addTestStream(t, muxer)
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("WriteHeader() wrote %d bytes, want 0", output.Len())
	}
	writeTestFrame(t, muxer, testFrame(4096, 0, 100))
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	info, err := streaminfo.Parse(output.Bytes()[8 : 8+streaminfo.Length])
	if err != nil {
		t.Fatal(err)
	}
	if info.MinBlockSize != 4096 || info.MaxBlockSize != 4096 {
		t.Fatalf("block sizes = (%d, %d), want (4096, 4096)", info.MinBlockSize, info.MaxBlockSize)
	}
	if info.TotalSamples != 0 {
		t.Fatalf("TotalSamples = %d, want 0", info.TotalSamples)
	}
}

func TestMuxerInfersS24BitsPerSampleWithoutStreamInfo(t *testing.T) {
	t.Parallel()
	var output nonSeekBuffer
	muxer := NewMuxer(&output, MuxerConfig{})
	if _, err := muxer.AddStream(media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecFLAC,
			Audio: media.AudioAttributes{
				SampleRate:    44100,
				Format:        media.SampleFormatS24,
				ChannelLayout: media.LayoutStereo2_0,
			},
		},
	}); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	info, err := streaminfo.Parse(output.Bytes()[8 : 8+streaminfo.Length])
	if err != nil {
		t.Fatal(err)
	}
	if info.BitsPerSample != 24 {
		t.Fatalf("BitsPerSample = %d, want 24", info.BitsPerSample)
	}
}

func TestMuxerExcludesFinalShortBlockFromMinimum(t *testing.T) {
	t.Parallel()
	file, err := os.CreateTemp(t.TempDir(), "stream-*.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	muxer := NewMuxer(file, MuxerConfig{})
	addTestStream(t, muxer)
	writeTestFrame(t, muxer, testFrame(4096, 0, 100))
	writeTestFrame(t, muxer, testFrame(4096, 1, 120))
	writeTestFrame(t, muxer, testFrame(1024, 2, 80))
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatalf("WriteTrailer() error = %v", err)
	}

	info := readTestStreamInfo(t, file)
	if info.MinBlockSize != 4096 || info.MaxBlockSize != 4096 {
		t.Fatalf("block sizes = (%d, %d), want (4096, 4096)", info.MinBlockSize, info.MaxBlockSize)
	}
	if info.TotalSamples != 9216 {
		t.Fatalf("TotalSamples = %d, want 9216", info.TotalSamples)
	}
}

func TestMuxerRoundtripTypedMetadata(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	muxer := NewMuxer(&output, MuxerConfig{})
	addTestStream(t, muxer)
	bundle := metadata.NewBundle()
	bundle.Set(metadata.KeyTitle("Song"))
	bundle.PushBack(metadata.KeyArtist("Artist"))
	bundle.Set(metadata.KeyThumbnail([]metadata.Thumbnail{{
		Data:        []byte{1, 2, 3},
		MIMEType:    "image/png",
		PictureType: metadata.PictureTypeFrontCover,
	}}))
	if err := muxer.SetMetadata(*bundle); err != nil {
		t.Fatal(err)
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}

	demuxer, err := NewDemuxer(bytes.NewReader(output.Bytes()), DemuxerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := demuxer.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	if title := metadata.Get[metadata.KeyTitle](&got); title != "Song" {
		t.Fatalf("title = %q", title)
	}
	if artists := metadata.Enumerate[metadata.KeyArtist](&got); len(artists) != 1 || artists[0] != "Artist" {
		t.Fatalf("artists = %#v", artists)
	}
	if thumbnails := metadata.Get[metadata.KeyThumbnail](&got); len(thumbnails) != 1 || !bytes.Equal(thumbnails[0].Data, []byte{1, 2, 3}) {
		t.Fatalf("thumbnails = %#v", thumbnails)
	}
}

func addTestStream(t *testing.T, muxer *Muxer) {
	t.Helper()
	_, err := muxer.AddStream(media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecFLAC,
			Audio: media.AudioAttributes{
				SampleRate:    44100,
				Format:        media.SampleFormatS16,
				BitsPerSample: 16,
				ChannelLayout: media.LayoutStereo2_0,
			},
		},
	})
	if err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
}

func writeTestFrame(t *testing.T, muxer *Muxer, data []byte) {
	t.Helper()
	packet := media.NewPacketFromData(data)
	defer packet.Release()
	if err := muxer.WritePacket(0, packet); err != nil {
		t.Fatalf("WritePacket() error = %v", err)
	}
}

func testFrame(blockSize, number, length int) []byte {
	blockSizeCode := byte(7)
	var extra []byte
	switch blockSize {
	case 256:
		blockSizeCode = 8
	case 1024:
		blockSizeCode = 10
	case 4096:
		blockSizeCode = 12
	default:
		extra = []byte{byte((blockSize - 1) >> 8), byte(blockSize - 1)}
	}
	header := []byte{0xff, 0xf8, blockSizeCode<<4 | 9, 0x18, byte(number)}
	header = append(header, extra...)
	header = append(header, hash.CRC8(header))
	frame := make([]byte, length)
	copy(frame, header)
	return frame
}

func readTestStreamInfo(t *testing.T, file *os.File) streaminfo.StreamInfo {
	t.Helper()
	if _, err := file.Seek(8, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, streaminfo.Length)
	if _, err := io.ReadFull(file, data); err != nil {
		t.Fatal(err)
	}
	info, err := streaminfo.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

type nonSeekBuffer struct{ bytes.Buffer }
