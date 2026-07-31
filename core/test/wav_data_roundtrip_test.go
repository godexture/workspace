package test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	wavpkg "github.com/godexture/godec/plugins/format-wav"
	"github.com/godexture/godec/sdk/testutil"
)

func TestWaveFilesInDataRoundtrip(t *testing.T) {
	t.Parallel()
	_, thisFile, _, _ := runtime.Caller(0)
	dataDir := filepath.Join(filepath.Dir(thisFile), "assets")
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dataDir, err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".wav" {
			continue
		}

		t.Run(e.Name(), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(dataDir, e.Name())
			orig, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", path, err)
			}

			demux, err := wavpkg.NewDemuxerEngine(bytes.NewReader(orig), wavpkg.MustNewDemuxerConfig())
			if err != nil {
				t.Fatalf("NewDemuxerEngine: %v", err)
			}

			streams, _, err := demux.Analyze()
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if len(streams) == 0 {
				t.Fatalf("no streams detected in %s", e.Name())
			}

			f := testutil.NewBuffer(nil)

			muxer, err := wavpkg.NewMuxerEngine(f, wavpkg.MustNewMuxerConfig())
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
				pkt, _, err := demux.ReadPacket()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("ReadPacket: %v", err)
				}
				if err := muxer.WritePacket(0, pkt); err != nil {
					t.Fatalf("WritePacket: %v", err)
				}
			}

			if err := muxer.WriteTrailer(); err != nil {
				t.Fatalf("WriteTrailer: %v", err)
			}

			remuxed := f.Bytes()

			if !bytes.Equal(orig, remuxed) {
				t.Fatalf("file %s roundtrip mismatch: original %d bytes vs remuxed %d bytes", e.Name(), len(orig), len(remuxed))
			}
		})
	}
}
