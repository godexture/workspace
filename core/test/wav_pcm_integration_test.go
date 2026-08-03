package test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	pcm "github.com/godexture/godec/plugin/pcm"
	wav "github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/sdk/engine"
	"github.com/godexture/godec/sdk/testutil"
)

func TestWaveFilesDemuxDecodeEncodeMuxRoundtrip(t *testing.T) {
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

			demuxer, err := wav.NewDemuxerEngine(bytes.NewReader(input), wav.MustNewDemuxerConfig())
			if err != nil {
				t.Fatalf("NewDemuxerEngine: %v", err)
			}

			streams, _, err := demuxer.Analyze()
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if len(streams) == 0 {
				t.Fatalf("no streams found in %s", e.Name())
			}

			decoder, err := pcm.NewDecoderEngine(streams[0], pcm.MustNewDecoderConfig())
			if err != nil {
				t.Fatalf("NewDecoderEngine: %v", err)
			}
			encoder := pcm.NewEncoderEngine(streams[0], pcm.MustNewEncoderConfig())

			f := testutil.NewBuffer(nil)

			muxer, err := wav.NewMuxerEngine(f, wav.MustNewMuxerConfig())
			if err != nil {
				t.Fatalf("NewMuxerEngine: %v", err)
			}
			if _, err := muxer.AddStream(streams[0]); err != nil {
				t.Fatalf("AddStream: %v", err)
			}
			if err := muxer.WriteHeader(); err != nil {
				t.Fatalf("WriteHeader: %v", err)
			}

			for {
				pkt, _, err := demuxer.ReadPacket()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("ReadPacket: %v", err)
				}

				if err := decoder.SendPacket(pkt); err != nil {
					t.Fatalf("decoder.SendPacket: %v", err)
				}

				if err := drainDecoderToMux(decoder, encoder, muxer); err != nil {
					t.Fatalf("drainDecoderToMux: %v", err)
				}
			}

			if err := decoder.Flush(); err != nil {
				t.Fatalf("decoder.Flush: %v", err)
			}
			if err := drainDecoderToMux(decoder, encoder, muxer); err != nil {
				t.Fatalf("final drainDecoderToMux: %v", err)
			}

			if err := encoder.Flush(); err != nil {
				t.Fatalf("encoder.Flush: %v", err)
			}
			if err := drainEncoderToMux(encoder, muxer); err != nil {
				t.Fatalf("final drainEncoderToMux: %v", err)
			}

			if err := muxer.WriteTrailer(); err != nil {
				t.Fatalf("WriteTrailer: %v", err)
			}

			remuxed := f.Bytes()

			if !bytes.Equal(input, remuxed) {
				t.Fatalf("integration roundtrip mismatch for %s: input=%d output=%d", e.Name(), len(input), len(remuxed))
			}
		})
	}
}

func drainDecoderToMux(decoder engine.DecoderEngine, encoder engine.EncoderEngine, muxer engine.MuxerEngine) error {
	for {
		frame, err := decoder.ReceiveFrame()
		if err == engine.ErrEAGAIN || err == engine.ErrEOF {
			return nil
		}
		if err != nil {
			return err
		}

		if err := encoder.SendFrame(&frame); err != nil {
			return err
		}
		if err := drainEncoderToMux(encoder, muxer); err != nil {
			return err
		}
	}
}

func drainEncoderToMux(encoder engine.EncoderEngine, muxer engine.MuxerEngine) error {
	for {
		pkt, err := encoder.ReceivePacket()
		if err == engine.ErrEAGAIN || err == engine.ErrEOF {
			return nil
		}
		if err != nil {
			return err
		}

		pkt.MediaType = media.MediaAudio
		if err := muxer.WritePacket(0, pkt); err != nil {
			return err
		}
	}
}
