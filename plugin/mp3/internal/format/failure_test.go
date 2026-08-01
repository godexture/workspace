package internal_test

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/plugin/mp3/internal/format"
	"github.com/godexture/godec/sdk/testutil/fault"
)

// Failure-injection baseline for docs/refactor/quality.md's "cancel、
// invalid input、Finalize/Close failure" M0 item, covering the MP3
// muxer/demuxer's I/O phases the same way plugin/wave's
// failure_test.go does for WAVE.

func mp3TestStream() media.StreamInfo {
	return media.StreamInfo{
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
}

func mp3TestMetadata() metadata.Bundle {
	meta := *metadata.NewBundle()
	meta.Set(metadata.KeyTitle("failure injection"))
	return meta
}

func readMP3Fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../test/testdata/l3-sin1k0db.mp3")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return data
}

func TestMuxerWriteHeaderPropagatesWriteFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected header write failure")
	w := fault.NewWriter(&bytes.Buffer{}, 0, wantErr)
	m := internal.NewMuxer(w, internal.MuxerConfig{})
	if _, err := m.AddStream(mp3TestStream()); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	// A non-empty ID3v2 tag is required for WriteHeader to actually write
	// anything; without metadata it is a documented no-op.
	if err := m.SetMetadata(mp3TestMetadata()); err != nil {
		t.Fatalf("SetMetadata() error = %v", err)
	}
	if err := m.WriteHeader(); !errors.Is(err, wantErr) {
		t.Fatalf("WriteHeader() error = %v, want %v", err, wantErr)
	}
}

func TestMuxerWritePacketPropagatesWriteFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected packet write failure")
	w := fault.NewWriter(&bytes.Buffer{}, 0, wantErr)
	m := internal.NewMuxer(w, internal.MuxerConfig{})
	if _, err := m.AddStream(mp3TestStream()); err != nil {
		t.Fatal(err)
	}
	packet := media.NewPacketFromData(readMP3Fixture(t))
	defer packet.Release()
	if err := m.WritePacket(0, packet); !errors.Is(err, wantErr) {
		t.Fatalf("WritePacket() error = %v, want %v", err, wantErr)
	}
}

func TestMuxerWriteTrailerPropagatesWriteFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected trailer write failure")
	buf := &bytes.Buffer{}
	w := fault.NewWriter(buf, 1<<20, wantErr)
	m := internal.NewMuxer(w, internal.MuxerConfig{})
	if _, err := m.AddStream(mp3TestStream()); err != nil {
		t.Fatal(err)
	}
	if err := m.SetMetadata(mp3TestMetadata()); err != nil {
		t.Fatal(err)
	}
	packet := media.NewPacketFromData(readMP3Fixture(t))
	if err := m.WritePacket(0, packet); err != nil {
		packet.Release()
		t.Fatal(err)
	}
	packet.Release()

	w.Fail()
	if err := m.WriteTrailer(); !errors.Is(err, wantErr) {
		t.Fatalf("WriteTrailer() error = %v, want %v", err, wantErr)
	}
}

func TestDemuxerAnalyzePropagatesReadFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected analyze read failure")
	data := readMP3Fixture(t)
	r := fault.NewReader(bytes.NewReader(data), 4, wantErr)
	demuxer, err := internal.NewDemuxer(r, internal.DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}
	if _, _, err := demuxer.Analyze(); !errors.Is(err, wantErr) {
		t.Fatalf("Analyze() error = %v, want %v", err, wantErr)
	}
}

func TestDemuxerReadPacketPropagatesReadFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected payload read failure")
	data := readMP3Fixture(t)
	r := fault.NewReader(bytes.NewReader(data), len(data)+1, wantErr)
	demuxer, err := internal.NewDemuxer(r, internal.DemuxerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := demuxer.Analyze(); err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	r.Fail()
	packet, _, err := demuxer.ReadPacket()
	if packet != nil {
		packet.Release()
	}
	if err == nil {
		t.Fatal("ReadPacket() succeeded, want the injected read failure to surface")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("ReadPacket() error = %v, want it to wrap %v", err, wantErr)
	}
}
