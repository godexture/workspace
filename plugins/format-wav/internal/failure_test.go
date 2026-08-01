package internal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/sdk/engine"
	"github.com/godexture/godec/sdk/testutil/audio"
	"github.com/godexture/godec/sdk/testutil/fault"
)

// TestMuxerWriteHeaderPropagatesWriteFailure is the WAVE mux/demux baseline
// for docs/refactor/quality.md's "Finalize/Close failure" M0 item: each
// externally-visible I/O phase must surface an injected write/read failure
// via errors.Is, not swallow or wrap it into an unrelated error, and must
// not panic or leave the muxer/demuxer usable afterward.
func TestMuxerWriteHeaderPropagatesWriteFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected header write failure")
	w := fault.NewSeekWriter(audio.NewBuffer(nil), 0, wantErr)
	m := NewMuxer(w, MuxerConfig{})
	if _, err := m.AddStream(testStream()); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := m.WriteHeader(); !errors.Is(err, wantErr) {
		t.Fatalf("WriteHeader() error = %v, want %v", err, wantErr)
	}
}

func TestMuxerWritePacketPropagatesWriteFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected packet write failure")
	buf := audio.NewBuffer(nil)
	// Let the header succeed (its size varies with the header path taken),
	// then fail every write from that point on.
	probe := NewMuxer(buf, MuxerConfig{})
	if _, err := probe.AddStream(testStream()); err != nil {
		t.Fatal(err)
	}
	if err := probe.WriteHeader(); err != nil {
		t.Fatal(err)
	}
	headerSize := len(buf.Bytes())
	buf.Reset()

	w := fault.NewSeekWriter(buf, headerSize, wantErr)
	m := NewMuxer(w, MuxerConfig{})
	if _, err := m.AddStream(testStream()); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	packet := media.NewPacket(8)
	defer packet.Release()
	if err := m.WritePacket(0, packet); !errors.Is(err, wantErr) {
		t.Fatalf("WritePacket() error = %v, want %v", err, wantErr)
	}
}

func TestMuxerWriteTrailerPropagatesWriteFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected trailer write failure")
	// A threshold well beyond header+packet size lets those phases succeed
	// normally; only the explicit Fail() below should trigger the failure.
	w := fault.NewSeekWriter(audio.NewBuffer(nil), 1<<20, wantErr)
	m := NewMuxer(w, MuxerConfig{})
	if _, err := m.AddStream(testStream()); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteHeader(); err != nil {
		t.Fatal(err)
	}
	packet := media.NewPacket(8)
	if err := m.WritePacket(0, packet); err != nil {
		packet.Release()
		t.Fatal(err)
	}
	packet.Release()

	// Fail from here on, covering both the trailer chunk write and the
	// seekable-path header back-patch inside WriteTrailer.
	w.Fail()
	if err := m.WriteTrailer(); !errors.Is(err, wantErr) {
		t.Fatalf("WriteTrailer() error = %v, want %v", err, wantErr)
	}
}

// TestMuxerWriteTrailerHeaderPatchFailureIsDistinguishable specifically
// targets WriteTrailer's seekable-path header back-patch (Seek + Write),
// which happens after the trailer chunk bytes, so trailer-chunk writes
// must succeed and only the header rewrite must fail.
func TestMuxerWriteTrailerHeaderPatchFailureIsDistinguishable(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected header patch failure")
	buf := audio.NewBuffer(nil)
	probe := NewMuxer(buf, MuxerConfig{})
	if _, err := probe.AddStream(testStream()); err != nil {
		t.Fatal(err)
	}
	if err := probe.WriteHeader(); err != nil {
		t.Fatal(err)
	}
	packet := media.NewPacket(8)
	if err := probe.WritePacket(0, packet); err != nil {
		packet.Release()
		t.Fatal(err)
	}
	packet.Release()
	sizeBeforeTrailer := len(buf.Bytes())
	buf.Reset()

	w := fault.NewSeekWriter(buf, sizeBeforeTrailer, wantErr)
	m := NewMuxer(w, MuxerConfig{})
	if _, err := m.AddStream(testStream()); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteHeader(); err != nil {
		t.Fatal(err)
	}
	packet2 := media.NewPacket(8)
	if err := m.WritePacket(0, packet2); err != nil {
		packet2.Release()
		t.Fatal(err)
	}
	packet2.Release()

	if err := m.WriteTrailer(); !errors.Is(err, wantErr) {
		t.Fatalf("WriteTrailer() error = %v, want %v (header back-patch write)", err, wantErr)
	}
}

func TestDemuxerAnalyzePropagatesReadFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("injected header read failure")
	full := buildTestWAV(t, []byte{0x10, 0x00, 0x20, 0x00})
	r := fault.NewReader(bytes.NewReader(full), 4, wantErr)
	demuxer, err := NewDemuxer(r, DemuxerConfig{})
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
	full := buildTestWAV(t, bytes.Repeat([]byte{0x10, 0x00, 0x20, 0x00}, 64))
	// A threshold beyond len(full) never trips on its own; Analyze() must
	// succeed first, and only the explicit Fail() below should trigger the
	// injected payload-read failure.
	r := fault.NewReader(bytes.NewReader(full), len(full)+1, wantErr)
	demuxer, err := NewDemuxer(r, DemuxerConfig{})
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
	if !errors.Is(err, wantErr) {
		t.Fatalf("ReadPacket() error = %v, want %v", err, wantErr)
	}
}

func testStream() media.StreamInfo {
	return media.StreamInfo{
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
}

// TestPipelineAggregatesRealMuxerFinalizeFailureWithSourceCloseFailure is
// docs/refactor/checkpoint.md M0#2's "primary failure と Finalize/Close
// failure を同時に発生させ、errors.Is で双方を保持することを検査する"
// requirement exercised against a real WAVE muxer (via sdk/engine.WrapMuxer)
// instead of only the generic lifecycleTestNode mock that
// core/pipeline/pipeline_test.go already covers this contract with.
//
// The source produces one packet then EOF (a clean, ordinary run), so the
// muxer's Start reaches WriteTrailer -- which fails because the underlying
// writer is configured to fail -- while the source's own Close
// independently fails for an unrelated reason (e.g. releasing an upstream
// resource). Pipeline.Run must return an error that is both.
func TestPipelineAggregatesRealMuxerFinalizeFailureWithSourceCloseFailure(t *testing.T) {
	t.Parallel()
	wantTrailerErr := errors.New("injected trailer write failure")
	wantSourceCloseErr := errors.New("injected source close failure")

	// Everything the pipeline does happens inside one Start() call, so the
	// only way to let WriteHeader+WritePacket succeed and just WriteTrailer
	// fail is a precise byte threshold, measured up front against the same
	// packet size the real run below sends.
	probeBuf := audio.NewBuffer(nil)
	probe := NewMuxer(probeBuf, MuxerConfig{})
	if _, err := probe.AddStream(testStream()); err != nil {
		t.Fatal(err)
	}
	if err := probe.WriteHeader(); err != nil {
		t.Fatal(err)
	}
	probePacket := media.NewPacket(8)
	if err := probe.WritePacket(0, probePacket); err != nil {
		probePacket.Release()
		t.Fatal(err)
	}
	probePacket.Release()

	// A seekable output is required: WriteTrailer's header back-patch (the
	// write this test targets) only happens on the seekable path.
	w := fault.NewSeekWriter(audio.NewBuffer(nil), len(probeBuf.Bytes()), wantTrailerErr)
	muxer := NewMuxer(w, MuxerConfig{})
	muxerNode := engine.WrapMuxer(muxer)

	source := &singlePacketSource{
		out:      node.NewOutPort[*media.Packet]("out", testStream()),
		closeErr: wantSourceCloseErr,
	}

	if _, err := muxerNode.AddStream(testStream()); err != nil {
		t.Fatalf("AddStream() error = %v", err)
	}
	if err := pipeline.Link(source, "out", muxerNode, "in"); err != nil {
		t.Fatalf("Link() error = %v", err)
	}

	conversion, err := pipeline.New(source, muxerNode)
	if err != nil {
		t.Fatalf("pipeline.New() error = %v", err)
	}
	runErr := conversion.Run(context.Background())
	if !errors.Is(runErr, wantTrailerErr) {
		t.Fatalf("Run() error = %v, want it to wrap the muxer's WriteTrailer failure %v", runErr, wantTrailerErr)
	}
	if !errors.Is(runErr, wantSourceCloseErr) {
		t.Fatalf("Run() error = %v, want it to also wrap the source's Close failure %v", runErr, wantSourceCloseErr)
	}
}

// singlePacketSource emits exactly one data packet then EOF, with a
// caller-controlled Close error, standing in for a demux/decode node whose
// own cleanup can fail independently of whatever the downstream mux node
// does.
type singlePacketSource struct {
	out      *node.OutPort[*media.Packet]
	sent     bool
	closeErr error
}

func (s *singlePacketSource) OutputPorts() map[string]*node.OutPort[*media.Packet] {
	return map[string]*node.OutPort[*media.Packet]{"out": s.out}
}

func (s *singlePacketSource) Start(ctx context.Context) error {
	defer s.out.Edge().Close()
	if s.sent {
		return nil
	}
	s.sent = true
	packet := media.NewPacket(8)
	return s.out.Push(ctx, packet)
}

func (s *singlePacketSource) Close() error { return s.closeErr }

var _ io.Closer = (*singlePacketSource)(nil)
