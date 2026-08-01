package internal

import (
	"bytes"
	"errors"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/sdk/testutil/fault"
)

// Failure-injection baseline for docs/refactor/quality.md's "cancel、
// invalid input、Finalize/Close failure" M0 item, covering the FLAC
// muxer/demuxer I/O phases the same way plugins/format-wav and
// plugins/format-mp3's failure_test.go do. FLAC's own WriteHeader only
// prepares in-memory state (see muxer.go's writeHeader vs. WriteHeader);
// the first byte actually reaches the writer inside WritePacket or
// WriteTrailer, so those are where a write failure is injected.

func TestMuxerWritePacketPropagatesWriteFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected first-frame write failure")
	w := fault.NewWriter(&bytes.Buffer{}, 0, wantErr)
	muxer := NewMuxer(w, MuxerConfig{})
	addTestStream(t, muxer)

	packet := media.NewPacketFromData(testFrame(4096, 0, 100))
	defer packet.Release()
	if err := muxer.WritePacket(0, packet); !errors.Is(err, wantErr) {
		t.Fatalf("WritePacket() error = %v, want %v", err, wantErr)
	}
}

func TestMuxerWriteTrailerPropagatesWriteFailureOnEmptyStream(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected empty-stream trailer write failure")
	w := fault.NewSeekWriter(&seekableBuffer{}, 0, wantErr)
	muxer := NewMuxer(w, MuxerConfig{})
	addTestStream(t, muxer)

	// No frames written: WriteTrailer must itself perform the deferred
	// header write (FLAC marker + STREAMINFO) for a zero-length stream.
	if err := muxer.WriteTrailer(); !errors.Is(err, wantErr) {
		t.Fatalf("WriteTrailer() error = %v, want %v", err, wantErr)
	}
}

func TestMuxerWriteTrailerPropagatesStreamInfoPatchFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected streaminfo patch failure")
	buf := &seekableBuffer{}
	probe := NewMuxer(buf, MuxerConfig{})
	addTestStream(t, probe)
	probePacket := media.NewPacketFromData(testFrame(4096, 0, 100))
	if err := probe.WritePacket(0, probePacket); err != nil {
		probePacket.Release()
		t.Fatal(err)
	}
	probePacket.Release()
	sizeBeforeTrailer := buf.Len()
	buf.Reset()

	w := fault.NewSeekWriter(buf, sizeBeforeTrailer, wantErr)
	muxer := NewMuxer(w, MuxerConfig{})
	addTestStream(t, muxer)
	packet := media.NewPacketFromData(testFrame(4096, 0, 100))
	if err := muxer.WritePacket(0, packet); err != nil {
		packet.Release()
		t.Fatal(err)
	}
	packet.Release()

	// The trailer chunk write above already consumed the threshold, so
	// only the seekable-path STREAMINFO back-patch (Seek(8) + Write) that
	// WriteTrailer performs next should fail.
	if err := muxer.WriteTrailer(); !errors.Is(err, wantErr) {
		t.Fatalf("WriteTrailer() error = %v, want %v (STREAMINFO back-patch)", err, wantErr)
	}
}

func TestDemuxerReadPacketPropagatesReadFailureAfterAnalyze(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected payload read failure")
	input := makeTestFLAC(t, 0, nil)
	r := &failingReadSeeker{Reader: bytes.NewReader(input), failAfter: len(input) + 1, err: wantErr}
	demuxer, err := NewDemuxer(r, DemuxerConfig{})
	if err != nil {
		t.Fatalf("NewDemuxer() error = %v", err)
	}
	r.failAfter = 0
	packet, _, err := demuxer.ReadPacket()
	if packet != nil {
		packet.Release()
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("ReadPacket() error = %v, want %v", err, wantErr)
	}
}

// seekableBuffer is a minimal in-memory io.WriteSeeker, since
// fault.SeekWriter needs a real WriteSeeker to wrap and bytes.Buffer isn't
// seekable.
type seekableBuffer struct {
	data []byte
	pos  int
}

func (b *seekableBuffer) Write(p []byte) (int, error) {
	end := b.pos + len(p)
	if end > len(b.data) {
		grown := make([]byte, end)
		copy(grown, b.data)
		b.data = grown
	}
	copy(b.data[b.pos:end], p)
	b.pos = end
	return len(p), nil
}

func (b *seekableBuffer) Seek(offset int64, whence int) (int64, error) {
	var target int64
	switch whence {
	case 0:
		target = offset
	case 1:
		target = int64(b.pos) + offset
	case 2:
		target = int64(len(b.data)) + offset
	}
	if target < 0 {
		return 0, errors.New("seekableBuffer: negative position")
	}
	b.pos = int(target)
	return target, nil
}

func (b *seekableBuffer) Len() int { return len(b.data) }
func (b *seekableBuffer) Reset()   { b.data = nil; b.pos = 0 }
