//go:generate go run ./snapshot-generator

package test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"testing"

	mp3codec "github.com/godexture/codec-mp3"
	"github.com/godexture/codec-mp3/test/config"
	"github.com/godexture/core/domain/media"
	mp3format "github.com/godexture/format-mp3"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil"
)

const maxAllowedDiff = 1e-6

func Test_Snapshots(t *testing.T) {
	for _, fileName := range config.EnumerateTestdataFiles() {
		t.Run(fileName, func(t *testing.T) {
			t.Parallel()

			dataPath := config.BuildTestdataPath(fileName)
			data, err := os.ReadFile(dataPath)
			if err != nil {
				t.Fatalf("failed to read test MP3 file: %v", err)
			}

			actual, err := decode(data)
			if err != nil {
				t.Fatalf("failed to decode MP3 data: %v", err)
			}

			expected, err := testutil.LoadSnapshot(config.BuildSnapshotPath(fileName))
			if err != nil {
				t.Fatalf("failed to load snapshot: %v", err)
			}

			if err := testutil.ComparePCM(actual, expected, maxAllowedDiff); err != nil {
				t.Errorf("PCM comparison failed: %v", err)
			}
		})
	}
}

func decode(data []byte) ([]float32, error) {
	demuxer, err := mp3format.NewDemuxerEngine(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	_, _, err = demuxer.Analyze()
	if err != nil {
		return nil, err
	}

	decoder := mp3codec.NewDecoderEngine(mp3codec.DecoderConfig{})

	var pcm []float32

	for {
		packet, _, err := demuxer.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if err := decoder.SendPacket(packet); err != nil {
			if err != engine.ErrEAGAIN {
				return nil, err
			}
		}

		for {
			frame, err := decoder.ReceiveFrame()
			if err == engine.ErrEAGAIN {
				break
			}
			if err != nil {
				return nil, err
			}

			audioFrame, isAudioFrame := (*frame).(*media.AudioFrame)
			if !isAudioFrame {
				return nil, fmt.Errorf("expected AudioFrame")
			}

			plane := audioFrame.Planes()[0]
			channels := audioFrame.Layout.ChannelCount()
			samples := audioFrame.Samples
			totalSamples := samples * channels

			for i := 0; i < totalSamples; i++ {
				sample := math.Float32frombits(binary.LittleEndian.Uint32(plane[i<<2 : (i+1)<<2]))
				pcm = append(pcm, sample)
			}
		}
	}

	// Flush
	if err := decoder.Flush(); err != nil {
		return nil, err
	}

	for {
		frame, err := decoder.ReceiveFrame()
		if err == engine.ErrEOF || err == engine.ErrEAGAIN {
			break
		}
		if err != nil {
			return nil, err
		}

		audioFrame, isAudioFrame := (*frame).(*media.AudioFrame)
		if !isAudioFrame {
			return nil, fmt.Errorf("expected AudioFrame")
		}

		plane := audioFrame.Planes()[0]
		channels := audioFrame.Layout.ChannelCount()
		samples := audioFrame.Samples
		totalSamples := samples * channels

		for i := 0; i < totalSamples; i++ {
			sample := math.Float32frombits(binary.LittleEndian.Uint32(plane[i<<2 : (i+1)<<2]))
			pcm = append(pcm, sample)
		}
	}

	return pcm, nil
}

