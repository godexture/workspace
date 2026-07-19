package audio

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil/audio/nodes"
	"github.com/godexture/sdk/testutil/audio/pcm"
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

// RunSnapshotTests runs applicable demux-decode and encode-mux snapshot tests.
func RunSnapshotTests(t *testing.T, cfg SnapshotConfig) {
	t.Helper()
	stream, err := resolveStreamInfo(cfg.MediaPath, cfg.Demux, cfg.StreamInfo)
	if err != nil {
		t.Fatalf("failed to resolve stream info: %v", err)
	}

	if cfg.Demux != nil && cfg.Decode != nil {
		t.Run("DemuxDecode", func(t *testing.T) {
			if err := runSnapshotDemuxDecode(t.Context(), cfg, stream); err != nil {
				t.Errorf("PCM comparison failed: %v", err)
			}
		})
	}
	if cfg.Encode != nil && cfg.Mux != nil {
		t.Run("EncodeMux", func(t *testing.T) {
			if err := runSnapshotEncodeMux(t.Context(), t, t.TempDir(), cfg, stream); err != nil {
				t.Errorf("PCM comparison failed: %v", err)
			}
		})
	}
}

func runSnapshotDemuxDecode(ctx context.Context, cfg SnapshotConfig, stream media.StreamInfo) error {
	demux, err := openDemuxNode(cfg.MediaPath, cfg.Demux, &stream)
	if err != nil {
		return err
	}
	defer demux.close()
	decoder := engine.WrapDecoder(cfg.Decode(stream))
	chunker := nodes.NewAudioChunk(4096) // pcmFramesPerChunk is 4096
	expected := newPCMSource(cfg.MediaPath, cfg.Expected, floatPCMAttributes(stream.Audio))
	compare := nodes.NewFrameCompare(cfg.Opts, false)
	if err := link(demux.node, "out", decoder, "in"); err != nil {
		return err
	}
	if err := link(decoder, "out", chunker, "in"); err != nil {
		return err
	}
	if err := link(chunker, "out", compare, "actual"); err != nil {
		return err
	}
	if err := link(expected, "out", compare, "expected"); err != nil {
		return err
	}
	return runNodes(ctx, demux.node, decoder, chunker, expected, compare)
}

func runSnapshotEncodeMux(ctx context.Context, tester testing.TB, tempDir string, cfg SnapshotConfig, stream media.StreamInfo) error {
	output, err := createIntermediateFile(tempDir, cfg.MediaPath)
	if err != nil {
		return err
	}
	outputPath := output.Name()
	defer os.Remove(outputPath)
	source := newPCMSource(cfg.MediaPath, cfg.Expected, stream.Audio)
	encoder := engine.WrapEncoder(cfg.Encode())
	muxer, err := wrapMux(cfg.Mux, output, stream)
	if err != nil {
		output.Close()
		return err
	}
	if err := link(source, "out", encoder, "in"); err != nil {
		output.Close()
		return err
	}
	if err := link(encoder, "out", muxer, "in"); err != nil {
		output.Close()
		return err
	}
	if err := runNodes(ctx, source, encoder, muxer); err != nil {
		output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	if cfg.Tester != nil {
		cfg.Tester(tester, outputPath)
	}

	expected := newPCMSource(cfg.MediaPath, cfg.Expected, floatPCMAttributes(stream.Audio))
	actual := nodes.NewFFmpegPCMSource(outputPath, floatPCMAttributes(stream.Audio))
	compare := nodes.NewFrameCompare(cfg.Opts, false)
	if err := link(expected, "out", compare, "expected"); err != nil {
		return err
	}
	if err := link(actual, "out", compare, "actual"); err != nil {
		return err
	}
	return runNodes(ctx, expected, actual, compare)
}

// RunRoundtripTests runs one format, codec, and full roundtrip for the
// applicable factories. Equivalent reverse-named duplicate cases are omitted.
func RunRoundtripTests(t *testing.T, cfg RoundtripConfig) {
	t.Helper()
	stream, err := resolveStreamInfo(cfg.MediaPath, cfg.Demux, cfg.StreamInfo)
	if err != nil {
		t.Fatalf("failed to resolve stream info: %v", err)
	}
	if cfg.Demux != nil && cfg.Mux != nil {
		t.Run("DemuxMux", func(t *testing.T) {
			if err := runRoundtripDemuxMux(t.Context(), t.TempDir(), cfg, stream); err != nil {
				t.Errorf("format roundtrip failed: %v", err)
			}
		})
	}
	if cfg.Demux != nil && cfg.Decode != nil && cfg.Encode != nil {
		t.Run("DecodeEncode", func(t *testing.T) {
			if err := runRoundtripDecodeEncode(t.Context(), cfg, stream); err != nil {
				t.Errorf("codec roundtrip failed: %v", err)
			}
		})
	}
	if cfg.Demux != nil && cfg.Decode != nil && cfg.Encode != nil && cfg.Mux != nil {
		t.Run("DemuxDecodeEncodeMux", func(t *testing.T) {
			if err := runRoundtripFull(t.Context(), t, t.TempDir(), cfg, stream); err != nil {
				t.Errorf("full roundtrip failed: %v", err)
			}
		})
	}
}

func runRoundtripDemuxMux(ctx context.Context, tempDir string, cfg RoundtripConfig, stream media.StreamInfo) error {
	output, err := createIntermediateFile(tempDir, cfg.MediaPath)
	if err != nil {
		return err
	}
	outputPath := output.Name()
	defer os.Remove(outputPath)
	source, err := openDemuxNode(cfg.MediaPath, cfg.Demux, cfg.StreamInfo)
	if err != nil {
		output.Close()
		return err
	}
	muxer, err := wrapMux(cfg.Mux, output, stream)
	if err != nil {
		source.close()
		output.Close()
		return err
	}
	if err := link(source.node, "out", muxer, "in"); err != nil {
		source.close()
		output.Close()
		return err
	}
	err = runNodes(ctx, source.node, muxer)
	source.close()
	closeErr := output.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	expected, err := openDemuxNode(cfg.MediaPath, cfg.Demux, cfg.StreamInfo)
	if err != nil {
		return err
	}
	defer expected.close()
	actual, err := openDemuxNode(outputPath, cfg.Demux, nil)
	if err != nil {
		return err
	}
	defer actual.close()
	compare := nodes.NewPacketCompare()
	if err := link(expected.node, "out", compare, "expected"); err != nil {
		return err
	}
	if err := link(actual.node, "out", compare, "actual"); err != nil {
		return err
	}
	return runNodes(ctx, expected.node, actual.node, compare)
}

func runRoundtripDecodeEncode(ctx context.Context, cfg RoundtripConfig, stream media.StreamInfo) error {
	source, err := openDemuxNode(cfg.MediaPath, cfg.Demux, cfg.StreamInfo)
	if err != nil {
		return err
	}
	defer source.close()
	decoder1 := engine.WrapDecoder(cfg.Decode(stream))
	chunker1 := nodes.NewAudioChunk(4096)
	tee := nodes.NewFrameTee()
	encoder := engine.WrapEncoder(cfg.Encode())
	decoder2 := engine.WrapDecoder(cfg.Decode(stream))
	chunker2 := nodes.NewAudioChunk(4096)
	compare := nodes.NewFrameCompare(cfg.Opts, true)
	links := []error{
		link(source.node, "out", decoder1, "in"),
		link(decoder1, "out", chunker1, "in"),
		link(chunker1, "out", tee, "in"),
		link(tee, "first", compare, "expected"),
		link(tee, "second", encoder, "in"),
		link(encoder, "out", decoder2, "in"),
		link(decoder2, "out", chunker2, "in"),
		link(chunker2, "out", compare, "actual"),
	}
	for _, err := range links {
		if err != nil {
			return err
		}
	}
	return runNodes(ctx, source.node, decoder1, chunker1, tee, encoder, decoder2, chunker2, compare)
}

func runRoundtripFull(ctx context.Context, tester testing.TB, tempDir string, cfg RoundtripConfig, stream media.StreamInfo) error {
	output, err := createIntermediateFile(tempDir, cfg.MediaPath)
	if err != nil {
		return err
	}
	outputPath := output.Name()
	defer os.Remove(outputPath)
	source, err := openDemuxNode(cfg.MediaPath, cfg.Demux, cfg.StreamInfo)
	if err != nil {
		output.Close()
		return err
	}
	decoder := engine.WrapDecoder(cfg.Decode(stream))
	chunker := nodes.NewAudioChunk(4096)
	encoder := engine.WrapEncoder(cfg.Encode())
	muxer, err := wrapMux(cfg.Mux, output, stream)
	if err != nil {
		source.close()
		output.Close()
		return err
	}
	links := []error{
		link(source.node, "out", decoder, "in"),
		link(decoder, "out", chunker, "in"),
		link(chunker, "out", encoder, "in"),
		link(encoder, "out", muxer, "in"),
	}
	for _, err := range links {
		if err != nil {
			source.close()
			output.Close()
			return err
		}
	}
	err = runNodes(ctx, source.node, decoder, chunker, encoder, muxer)
	source.close()
	closeErr := output.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if cfg.Tester != nil {
		cfg.Tester(tester, outputPath)
	}

	expectedDemux, err := openDemuxNode(cfg.MediaPath, cfg.Demux, cfg.StreamInfo)
	if err != nil {
		return err
	}
	defer expectedDemux.close()
	actualDemux, err := openDemuxNode(outputPath, cfg.Demux, nil)
	if err != nil {
		return err
	}
	defer actualDemux.close()
	expectedDecoder := engine.WrapDecoder(cfg.Decode(stream))
	actualDecoder := engine.WrapDecoder(cfg.Decode(stream))
	expectedChunker := nodes.NewAudioChunk(4096)
	actualChunker := nodes.NewAudioChunk(4096)
	compare := nodes.NewFrameCompare(cfg.Opts, true)
	links = []error{
		link(expectedDemux.node, "out", expectedDecoder, "in"),
		link(actualDemux.node, "out", actualDecoder, "in"),
		link(expectedDecoder, "out", expectedChunker, "in"),
		link(actualDecoder, "out", actualChunker, "in"),
		link(expectedChunker, "out", compare, "expected"),
		link(actualChunker, "out", compare, "actual"),
	}
	for _, err := range links {
		if err != nil {
			return err
		}
	}
	return runNodes(ctx, expectedDemux.node, actualDemux.node, expectedDecoder, actualDecoder, expectedChunker, actualChunker, compare)
}
