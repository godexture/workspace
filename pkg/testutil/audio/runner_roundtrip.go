package audio

import (
	"context"
	"os"
	"testing"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil/audio/nodes"
)

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
