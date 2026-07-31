package audio

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/sdk/engine"
	"github.com/godexture/godec/sdk/testutil/audio/nodes"
	"github.com/godexture/godec/sdk/testutil/audio/pcm"
)

type DemuxerFactory = func(io.ReadSeeker) (engine.DemuxerEngine, error)
type DecoderFactory = func(media.StreamInfo) engine.DecoderEngine
type EncoderFactory = func() engine.EncoderEngine
type MuxerFactory = func(io.Writer) engine.MuxerEngine
type OutputTester = func(testing.TB, string)

type DecodeConfig struct {
	MediaPath  string
	StreamInfo *media.StreamInfo // Optional override
	Demux      DemuxerFactory
	Decode     DecoderFactory
}

type SnapshotConfig struct {
	MediaPath  string
	Expected   []float32
	Opts       pcm.CompareOptions
	StreamInfo *media.StreamInfo // Optional override
	Tester     OutputTester

	Demux  DemuxerFactory
	Decode DecoderFactory
	Encode EncoderFactory
	Mux    MuxerFactory
}

type RoundtripConfig struct {
	MediaPath  string
	Opts       pcm.CompareOptions
	StreamInfo *media.StreamInfo // Optional override
	Tester     OutputTester

	Demux  DemuxerFactory
	Decode DecoderFactory
	Encode EncoderFactory
	Mux    MuxerFactory
}

type openedDemux struct {
	node   node.Demuxer
	file   *os.File
	stream media.StreamInfo
}

func (d *openedDemux) close() error { return d.file.Close() }

func openDemuxNode(path string, factory DemuxerFactory, override *media.StreamInfo) (*openedDemux, error) {
	if factory == nil {
		return nil, fmt.Errorf("demux factory is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open media file: %w", err)
	}
	demuxEngine, err := factory(file)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("initialize demuxer: %w", err)
	}
	demuxNode := engine.WrapDemuxer(demuxEngine)
	streams, err := demuxNode.Streams()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("analyze media: %w", err)
	}
	if len(streams) == 0 {
		file.Close()
		return nil, fmt.Errorf("no streams found in media file")
	}
	stream := streams[0]
	if override != nil {
		stream = *override
	}
	return &openedDemux{node: demuxNode, file: file, stream: stream}, nil
}

func resolveStreamInfo(path string, factory DemuxerFactory, override *media.StreamInfo) (media.StreamInfo, error) {
	if override != nil {
		return *override, nil
	}
	demux, err := openDemuxNode(path, factory, nil)
	if err != nil {
		return media.StreamInfo{}, err
	}
	defer demux.close()
	return demux.stream, nil
}

func wrapMux(factory MuxerFactory, writer io.Writer, stream media.StreamInfo) (node.Muxer, error) {
	if factory == nil {
		return nil, fmt.Errorf("mux factory is required")
	}
	muxEngine := factory(writer)
	if muxEngine == nil {
		return nil, fmt.Errorf("mux factory returned nil")
	}
	muxNode := engine.WrapMuxer(muxEngine)
	if _, err := muxNode.AddStream(stream); err != nil {
		return nil, fmt.Errorf("add mux stream: %w", err)
	}
	return muxNode, nil
}

func runNodes(ctx context.Context, nodes ...node.Node) error {
	pipeline, err := pipeline.New(nodes...)
	if err != nil {
		return err
	}
	return pipeline.Run(ctx)
}

func link[T any, A node.OutputNode[T], B node.InputNode[T]](from A, fromPort string, to B, toPort string) error {
	return pipeline.LinkWithBufferSize(from, fromPort, to, toPort, 1)
}

type frameSource interface {
	node.Node
	node.OutputNode[media.Frame]
}

func floatPCMAttributes(attrs media.AudioAttributes) media.AudioAttributes {
	attrs.Format = media.SampleFormatF32
	attrs.BitsPerSample = 32
	return attrs
}

func newPCMSource(path string, expected []float32, attrs media.AudioAttributes) frameSource {
	if expected != nil {
		return nodes.NewSlicePCMSource(expected, attrs)
	}
	return nodes.NewFFmpegPCMSource(path, attrs)
}

func createIntermediateFile(dir, mediaPath string) (*os.File, error) {
	return os.CreateTemp(dir, "testutil-*"+filepath.Ext(mediaPath))
}

// RunDecode streams a media file through a demuxer and decoder and discards
// decoded frames. It returns engine errors so conformance tests can inspect them.
func RunDecode(ctx context.Context, cfg DecodeConfig) error {
	if cfg.Decode == nil {
		return fmt.Errorf("decode factory is required")
	}
	demux, err := openDemuxNode(cfg.MediaPath, cfg.Demux, cfg.StreamInfo)
	if err != nil {
		return err
	}
	defer demux.close()
	decoder := engine.WrapDecoder(cfg.Decode(demux.stream))
	discard := nodes.NewFrameDiscard()
	if err := link(demux.node, "out", decoder, "in"); err != nil {
		return err
	}
	if err := link(decoder, "out", discard, "in"); err != nil {
		return err
	}
	return runNodes(ctx, demux.node, decoder, discard)
}
