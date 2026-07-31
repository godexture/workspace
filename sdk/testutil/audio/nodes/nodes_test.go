package nodes_test

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/sdk/engine"
	"github.com/godexture/godec/sdk/testutil/audio"
	"github.com/godexture/godec/sdk/testutil/audio/nodes"
	"github.com/godexture/godec/sdk/testutil/audio/pcm"
)

func runNodes(ctx context.Context, ns ...node.Node) error {
	pipeline, err := pipeline.New(ns...)
	if err != nil {
		return err
	}
	return pipeline.Run(ctx)
}

func link[T any, A node.OutputNode[T], B node.InputNode[T]](from A, fromPort string, to B, toPort string) error {
	return pipeline.LinkWithBufferSize(from, fromPort, to, toPort, 1)
}

type chunkFrameSource struct {
	out    *node.OutPort[media.Frame]
	pcm    []float32
	chunks []int
	attrs  media.AudioAttributes
}

type packetSource struct {
	out     *node.OutPort[*media.Packet]
	packets [][]byte
}

type retainedFrame struct {
	media.ResourceBase
	released chan struct{}
}

func newRetainedFrame() *retainedFrame {
	frame := &retainedFrame{released: make(chan struct{})}
	frame.Init(func() { close(frame.released) })
	return frame
}

func (*retainedFrame) Pts() media.Pts { return 0 }

type retainedFrameSource struct {
	out   *node.OutPort[media.Frame]
	frame media.Frame
}

func (n *retainedFrameSource) Start(ctx context.Context) error {
	defer n.out.Edge().Close()
	if err := n.out.Push(ctx, n.frame); err != nil {
		n.frame.Release()
		return err
	}
	return nil
}

func (*retainedFrameSource) Close() error { return nil }

func (n *retainedFrameSource) OutputPorts() map[string]*node.OutPort[media.Frame] {
	return map[string]*node.OutPort[media.Frame]{"out": n.out}
}

func newPacketSource(packets ...[]byte) *packetSource {
	return &packetSource{out: node.NewOutPort[*media.Packet]("out", media.StreamInfo{}), packets: packets}
}

func (n *packetSource) OutputPorts() map[string]*node.OutPort[*media.Packet] {
	return map[string]*node.OutPort[*media.Packet]{"out": n.out}
}

func (n *packetSource) Start(ctx context.Context) error {
	defer n.out.Edge().Close()
	for _, data := range n.packets {
		packet := media.NewPacket(len(data))
		copy(packet.Data(), data)
		if err := n.out.Push(ctx, packet); err != nil {
			return err
		}
	}
	return nil
}

func (n *packetSource) Close() error { return nil }

func newChunkFrameSource(pcmData []float32, chunks []int, attrs media.AudioAttributes) *chunkFrameSource {
	return &chunkFrameSource{
		out:    node.NewOutPort[media.Frame]("out", media.StreamInfo{}),
		pcm:    pcmData,
		chunks: chunks,
		attrs:  attrs,
	}
}

func (n *chunkFrameSource) OutputPorts() map[string]*node.OutPort[media.Frame] {
	return map[string]*node.OutPort[media.Frame]{"out": n.out}
}

func (n *chunkFrameSource) Start(ctx context.Context) error {
	defer n.out.Edge().Close()
	offset := 0
	for _, size := range n.chunks {
		end := min(offset+size, len(n.pcm))
		frame, err := pcm.CreateAudioFrame(n.pcm[offset:end], n.attrs)
		if err != nil {
			return err
		}
		if err := n.out.Push(ctx, *frame); err != nil {
			return err
		}
		offset = end
	}
	if offset != len(n.pcm) {
		return errors.New("test chunks did not consume all PCM")
	}
	return nil
}

func (n *chunkFrameSource) Close() error { return nil }

func TestFrameCompareAcrossDifferentBoundaries(t *testing.T) {
	t.Parallel()
	attrs := media.AudioAttributes{SampleRate: 48000, Format: media.SampleFormatF32, BitsPerSample: 32, ChannelLayout: media.LayoutMono1}
	pcmData := make([]float32, 17)
	for i := range pcmData {
		pcmData[i] = float32(i-8) / 16
	}
	expected := newChunkFrameSource(pcmData, []int{3, 5, 9}, attrs)
	actual := newChunkFrameSource(pcmData, []int{7, 2, 8}, attrs)
	compare := nodes.NewFrameCompare(pcm.CompareOptions{MaxAbsDiff: 1e-6, MaxRMSE: 1e-6, MinSNR: 100}, true)
	if err := link(expected, "out", compare, "expected"); err != nil {
		t.Fatal(err)
	}
	if err := link(actual, "out", compare, "actual"); err != nil {
		t.Fatal(err)
	}
	if err := runNodes(t.Context(), expected, actual, compare); err != nil {
		t.Fatal(err)
	}
}

func TestAudioChunkNodeCombinesAndSplitsWithoutChangingPCM(t *testing.T) {
	t.Parallel()
	attrs := media.AudioAttributes{SampleRate: 48000, Format: media.SampleFormatS16, BitsPerSample: 16, ChannelLayout: media.LayoutMono1}
	pcmData := make([]float32, 17)
	for i := range pcmData {
		pcmData[i] = float32(i-8) / 16
	}
	expected := newChunkFrameSource(pcmData, []int{17}, attrs)
	input := newChunkFrameSource(pcmData, []int{3, 5, 9}, attrs)
	chunker := nodes.NewAudioChunk(4)
	compare := nodes.NewFrameCompare(pcm.CompareOptions{MaxAbsDiff: 1e-6, MaxRMSE: 1e-6, MinSNR: 100}, true)
	if err := link(input, "out", chunker, "in"); err != nil {
		t.Fatal(err)
	}
	if err := link(chunker, "out", compare, "actual"); err != nil {
		t.Fatal(err)
	}
	if err := link(expected, "out", compare, "expected"); err != nil {
		t.Fatal(err)
	}
	if err := runNodes(t.Context(), input, chunker, expected, compare); err != nil {
		t.Fatal(err)
	}
}

func TestFrameTeeBalancesReferences(t *testing.T) {
	t.Parallel()
	frame := newRetainedFrame()
	var wrapped media.Frame = frame
	source := &retainedFrameSource{out: node.NewOutPort[media.Frame]("out", media.StreamInfo{}), frame: wrapped}
	tee := nodes.NewFrameTee()
	first := nodes.NewFrameDiscard()
	second := nodes.NewFrameDiscard()
	links := []error{
		link(source, "out", tee, "in"),
		link(tee, "first", first, "in"),
		link(tee, "second", second, "in"),
	}
	for _, err := range links {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := runNodes(t.Context(), source, tee, first, second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-frame.released:
	default:
		t.Fatal("tee pipeline did not release its input frame")
	}
}

func TestFrameComparePreservesRoundtripLengthRule(t *testing.T) {
	t.Parallel()
	attrs := media.AudioAttributes{SampleRate: 48000, Format: media.SampleFormatF32, BitsPerSample: 32, ChannelLayout: media.LayoutMono1}
	expected := newChunkFrameSource([]float32{0, 0, 0}, []int{3}, attrs)
	actual := newChunkFrameSource([]float32{0, 0}, []int{2}, attrs)
	compare := nodes.NewFrameCompare(pcm.CompareOptions{MaxAbsDiff: 1e-6, MaxRMSE: 1e-6, MinSNR: 100}, true)
	if err := link(expected, "out", compare, "expected"); err != nil {
		t.Fatal(err)
	}
	if err := link(actual, "out", compare, "actual"); err != nil {
		t.Fatal(err)
	}
	err := runNodes(t.Context(), expected, actual, compare)
	if err == nil || !strings.Contains(err.Error(), "length mismatch") {
		t.Fatalf("expected length mismatch, got %v", err)
	}
}

func TestPacketCompareRejectsMismatch(t *testing.T) {
	t.Parallel()
	expected := newPacketSource([]byte("first"), []byte("expected"))
	actual := newPacketSource([]byte("first"), []byte("actual"))
	compare := nodes.NewPacketCompare()
	if err := link(expected, "out", compare, "expected"); err != nil {
		t.Fatal(err)
	}
	if err := link(actual, "out", compare, "actual"); err != nil {
		t.Fatal(err)
	}
	err := runNodes(t.Context(), expected, actual, compare)
	if err == nil || !strings.Contains(err.Error(), "byte stream mismatch") {
		t.Fatalf("expected packet mismatch, got %v", err)
	}
}

func TestPacketCompareAcrossDifferentBoundaries(t *testing.T) {
	t.Parallel()
	expected := newPacketSource([]byte("abc"), []byte("defgh"), []byte("i"))
	actual := newPacketSource([]byte("a"), []byte("bcdef"), []byte("ghi"))
	compare := nodes.NewPacketCompare()
	if err := link(expected, "out", compare, "expected"); err != nil {
		t.Fatal(err)
	}
	if err := link(actual, "out", compare, "actual"); err != nil {
		t.Fatal(err)
	}
	if err := runNodes(t.Context(), expected, actual, compare); err != nil {
		t.Fatal(err)
	}
}

func TestFFmpegPCMSourceHonorsCanceledContext(t *testing.T) {
	t.Parallel()
	source := nodes.NewFFmpegPCMSource("missing-input.wav", media.AudioAttributes{
		SampleRate: 48000, Format: media.SampleFormatF32, BitsPerSample: 32, ChannelLayout: media.LayoutMono1,
	})
	discard := nodes.NewFrameDiscard()
	if err := link(source, "out", discard, "in"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := runNodes(ctx, source, discard)
	if err == nil {
		t.Fatal("expected canceled FFmpeg source to fail")
	}
}

type blockingDemuxer struct {
	consumed chan struct{}
	sent     bool
}

func (d *blockingDemuxer) Analyze() ([]media.StreamInfo, metadata.Bundle, error) {
	return []media.StreamInfo{{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{Audio: media.AudioAttributes{
			SampleRate: 48000, Format: media.SampleFormatS16, BitsPerSample: 16, ChannelLayout: media.LayoutMono1,
		}},
	}}, metadata.Bundle{}, nil
}

func (d *blockingDemuxer) ReadPacket() (*media.Packet, int, error) {
	if !d.sent {
		d.sent = true
		packet := media.NewPacket(2)
		return packet, 0, nil
	}
	select {
	case <-d.consumed:
		return nil, 0, io.EOF
	case <-time.After(time.Second):
		return nil, 0, errors.New("decoder did not consume first packet before second read")
	}
}

type signalingDecoder struct {
	consumed chan struct{}
	pending  bool
	flushed  bool
}

func (d *signalingDecoder) SendPacket(*media.Packet) error {
	if !d.pending {
		close(d.consumed)
	}
	d.pending = true
	return nil
}

func (d *signalingDecoder) ReceiveFrame() (media.Frame, error) {
	if d.pending {
		d.pending = false
		frame := media.NewAudioFrame(media.SampleFormatS16, media.LayoutMono1, 48000, 1)
		return frame, nil
	}
	if d.flushed {
		return nil, engine.ErrEOF
	}
	return nil, engine.ErrEAGAIN
}

func (d *signalingDecoder) Flush() error {
	d.flushed = true
	return nil
}

func TestRunDecodeStreamsBeforeDemuxEOF(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + string(os.PathSeparator) + "input.bin"
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	consumed := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	err := audio.RunDecode(ctx, audio.DecodeConfig{
		MediaPath: path,
		Demux: func(io.ReadSeeker) (engine.DemuxerEngine, error) {
			return &blockingDemuxer{consumed: consumed}, nil
		},
		Decode: func(media.StreamInfo) engine.DecoderEngine {
			return &signalingDecoder{consumed: consumed}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}
