package test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/pipeline"
	pcm "github.com/godexture/godec/plugin/pcm"
	wav "github.com/godexture/godec/plugin/wave"
	eng "github.com/godexture/godec/sdk/engine"
	"github.com/godexture/godec/sdk/testutil"
)

func TestPipeline_WavPcmRoundtrip(t *testing.T) {
	t.Parallel()
	_, thisFile, _, _ := runtime.Caller(0)
	dataDir := filepath.Join(filepath.Dir(thisFile), "assets")

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dataDir, err)
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".wav" {
			continue
		}

		t.Run(e.Name(), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(dataDir, e.Name())
			input, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", path, err)
			}

			demuxEngine, err := wav.NewDemuxerEngine(bytes.NewReader(input), wav.MustNewDemuxerConfig())
			if err != nil {
				t.Fatalf("NewDemuxerEngine: %v", err)
			}

			streams, meta, err := demuxEngine.Analyze()
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if len(streams) == 0 {
				t.Fatalf("no streams found in %s", e.Name())
			}

			decEngine, err := pcm.NewDecoderEngine(streams[0], pcm.MustNewDecoderConfig())
			if err != nil {
				t.Fatalf("NewDecoderEngine: %v", err)
			}
			encEngine := pcm.NewEncoderEngine(streams[0], pcm.MustNewEncoderConfig())

			f := testutil.NewBuffer(nil)

			muxEngine, err := wav.NewMuxerEngine(f, wav.MustNewMuxerConfig())
			if err != nil {
				t.Fatalf("NewMuxerEngine: %v", err)
			}
			if _, err := muxEngine.AddStream(streams[0]); err != nil {
				t.Fatalf("AddStream: %v", err)
			}
			if err := muxEngine.SetMetadata(meta); err != nil && err != io.EOF {
				t.Fatalf("SetMetadata: %v", err)
			}

			demuxNode := eng.WrapDemuxer(demuxEngine)
			decNode := eng.WrapDecoder(decEngine)
			encNode := eng.WrapEncoder(encEngine)
			muxNode := eng.WrapMuxer(muxEngine)

			if err := pipeline.Link(node.Demuxer(demuxNode), "out", node.Decoder(decNode), "in"); err != nil {
				t.Fatalf("link demux->dec: %v", err)
			}
			if err := pipeline.Link(node.Decoder(decNode), "out", node.Encoder(encNode), "in"); err != nil {
				t.Fatalf("link dec->enc: %v", err)
			}
			if err := pipeline.Link(node.Encoder(encNode), "out", node.Muxer(muxNode), "in"); err != nil {
				t.Fatalf("link enc->mux: %v", err)
			}

			conversion, err := pipeline.New(demuxNode, decNode, encNode, muxNode)
			if err != nil {
				t.Fatalf("pipeline.New: %v", err)
			}
			if err := conversion.Run(context.Background()); err != nil {
				t.Fatalf("pipeline.Run: %v", err)
			}

			remuxed := f.Bytes()

			if !bytes.Equal(input, remuxed) {
				t.Fatalf("runner roundtrip mismatch for %s: input=%d output=%d", e.Name(), len(input), len(remuxed))
			}
		})
	}
}
