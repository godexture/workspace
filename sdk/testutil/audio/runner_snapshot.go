package audio

import (
	"context"
	"os"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/sdk/engine"
	"github.com/godexture/godec/sdk/testutil/audio/nodes"
)

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
	chunker := nodes.NewAudioChunk(nodes.PcmFramesPerChunk)
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
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = output.Close()
		}
	}()
	source := newPCMSource(cfg.MediaPath, cfg.Expected, stream.Audio)
	encoder := engine.WrapEncoder(cfg.Encode())
	muxer, err := wrapMux(cfg.Mux, output, stream)
	if err != nil {
		return err
	}
	if err := link(source, "out", encoder, "in"); err != nil {
		return err
	}
	if err := link(encoder, "out", muxer, "in"); err != nil {
		return err
	}
	if err := runNodes(ctx, source, encoder, muxer); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	closeOnError = false
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
