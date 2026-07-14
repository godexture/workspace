package audio

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

type DemuxFunc = func([]byte) ([][]byte, error)
type MuxFunc = func([][]byte) ([]byte, error)
type DecodeFunc = func([][]byte) ([]float32, error)
type EncodeFunc = func([]float32) ([][]byte, error)

type SnapshotConfig struct {
	MediaPath  string
	Expected   []float32
	Opts       CompareOptions
	StreamInfo *media.StreamInfo // Optional override

	Demux  func(io.ReadSeeker) (engine.DemuxerEngine, error)
	Decode func(media.StreamInfo) engine.DecoderEngine
	Encode func() engine.EncoderEngine
	Mux    func(*Buffer) engine.MuxerEngine
}

type RoundtripConfig struct {
	MediaPath  string
	Opts       CompareOptions
	StreamInfo *media.StreamInfo // Optional override

	Demux  func(io.ReadSeeker) (engine.DemuxerEngine, error)
	Decode func(media.StreamInfo) engine.DecoderEngine
	Encode func() engine.EncoderEngine
	Mux    func(*Buffer) engine.MuxerEngine
}

func resolveStreamInfo(t *testing.T, mediaPath string, demuxFactory func(io.ReadSeeker) (engine.DemuxerEngine, error), override *media.StreamInfo) media.StreamInfo {
	if override != nil {
		return *override
	}
	if demuxFactory == nil {
		t.Fatalf("StreamInfo must be provided if Demux is nil")
	}
	mediaBytes, err := os.ReadFile(mediaPath)
	if err != nil {
		t.Fatalf("failed to read media file for stream info: %v", err)
	}
	demuxer, err := demuxFactory(bytes.NewReader(mediaBytes))
	if err != nil {
		t.Fatalf("failed to initialize demuxer for stream info: %v", err)
	}
	streams, _, err := demuxer.Analyze()
	if err != nil {
		t.Fatalf("failed to analyze stream info: %v", err)
	}
	if len(streams) == 0 {
		t.Fatalf("no streams found in media file")
	}
	return streams[0]
}

// RunSnapshotTests runs applicable demux-decode and encode-mux snapshot tests.
func RunSnapshotTests(t *testing.T, cfg SnapshotConfig) {
	t.Helper()

	var expected []float32
	if cfg.Expected == nil {
		file, err := os.Open(cfg.MediaPath)
		if err != nil {
			t.Fatalf("failed to open media file: %v", err)
		}

		expected, err = DecodeWithFFmpeg(file)
		if err != nil {
			t.Fatalf("failed to decode media file: %v", err)
		}
	} else {
		expected = cfg.Expected
	}

	var streamInfo media.StreamInfo
	if cfg.Decode != nil || cfg.Encode != nil || cfg.Mux != nil {
		streamInfo = resolveStreamInfo(t, cfg.MediaPath, cfg.Demux, cfg.StreamInfo)
	}

	var demux DemuxFunc
	if cfg.Demux != nil {
		demux = EngineDemux(cfg.Demux)
	}
	var decode DecodeFunc
	if cfg.Decode != nil {
		decode = EngineDecode(streamInfo, cfg.Decode)
	}
	var encode EncodeFunc
	if cfg.Encode != nil {
		encode = EngineEncode(streamInfo.Audio, cfg.Encode)
	}
	var mux MuxFunc
	if cfg.Mux != nil {
		mux = EngineMux(streamInfo, cfg.Mux)
	}

	if demux != nil && decode != nil {
		t.Run("DemuxDecode", func(t *testing.T) {
			RunSnapshotDemuxDecode(t, expected, cfg.MediaPath, cfg.Opts, demux, decode)
		})
	}

	if encode != nil && mux != nil {
		t.Run("EncodeMux", func(t *testing.T) {
			RunSnapshotEncodeMux(t, expected, cfg.Opts, encode, mux)
		})
	}
}

// RunRoundtripTests runs all applicable format and codec roundtrip tests.
func RunRoundtripTests(t *testing.T, cfg RoundtripConfig) {
	t.Helper()

	var streamInfo media.StreamInfo
	if cfg.Decode != nil || cfg.Encode != nil || cfg.Mux != nil {
		streamInfo = resolveStreamInfo(t, cfg.MediaPath, cfg.Demux, cfg.StreamInfo)
	}

	var demux DemuxFunc
	if cfg.Demux != nil {
		demux = EngineDemux(cfg.Demux)
	}
	var decode DecodeFunc
	if cfg.Decode != nil {
		decode = EngineDecode(streamInfo, cfg.Decode)
	}
	var encode EncodeFunc
	if cfg.Encode != nil {
		encode = EngineEncode(streamInfo.Audio, cfg.Encode)
	}
	var mux MuxFunc
	if cfg.Mux != nil {
		mux = EngineMux(streamInfo, cfg.Mux)
	}

	if demux != nil && mux != nil {
		t.Run("DemuxMux", func(t *testing.T) {
			t.Parallel()
			RunRoundtripDemuxMux(t, cfg.MediaPath, demux, mux)
		})
		t.Run("MuxDemux", func(t *testing.T) {
			t.Parallel()
			mediaBytes, err := os.ReadFile(cfg.MediaPath)
			if err != nil {
				t.Fatalf("failed to read media file: %v", err)
			}
			packets, err := demux(mediaBytes)
			if err != nil {
				t.Fatalf("failed to demux: %v", err)
			}
			RunRoundtripMuxDemux(t, packets, mux, demux)
		})
	}

	if demux != nil && decode != nil && encode != nil {
		t.Run("DecodeEncode", func(t *testing.T) {
			t.Parallel()
			mediaBytes, err := os.ReadFile(cfg.MediaPath)
			if err != nil {
				t.Fatalf("failed to read media file: %v", err)
			}
			packets, err := demux(mediaBytes)
			if err != nil {
				t.Fatalf("failed to demux: %v", err)
			}
			RunRoundtripDecodeEncode(t, packets, cfg.Opts, decode, encode)
		})
		t.Run("EncodeDecode", func(t *testing.T) {
			t.Parallel()
			mediaBytes, err := os.ReadFile(cfg.MediaPath)
			if err != nil {
				t.Fatalf("failed to read media file: %v", err)
			}
			packets, err := demux(mediaBytes)
			if err != nil {
				t.Fatalf("failed to demux: %v", err)
			}
			pcm, err := decode(packets)
			if err != nil {
				t.Fatalf("failed to decode: %v", err)
			}
			RunRoundtripEncodeDecode(t, pcm, cfg.Opts, encode, decode)
		})
	}

	if demux != nil && decode != nil && encode != nil && mux != nil {
		t.Run("DemuxDecodeEncodeMux", func(t *testing.T) {
			t.Parallel()
			RunRoundtripDemuxDecodeEncodeMux(t, cfg.MediaPath, cfg.Opts, demux, decode, encode, mux)
		})
		t.Run("EncodeMuxDemuxDecode", func(t *testing.T) {
			t.Parallel()
			mediaBytes, err := os.ReadFile(cfg.MediaPath)
			if err != nil {
				t.Fatalf("failed to read media file: %v", err)
			}
			packets, err := demux(mediaBytes)
			if err != nil {
				t.Fatalf("failed to demux: %v", err)
			}
			pcm, err := decode(packets)
			if err != nil {
				t.Fatalf("failed to decode: %v", err)
			}
			RunRoundtripEncodeMuxDemuxDecode(t, pcm, cfg.Opts, encode, mux, demux, decode)
		})
	}
}
