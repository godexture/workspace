//go:generate go run ./snapshot-generator

package test

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	mp3codec "github.com/godexture/codec-mp3"
	"github.com/godexture/codec-mp3/test/config"
	"github.com/godexture/core/domain/media"
	mp3format "github.com/godexture/format-mp3"
	"github.com/godexture/sdk/engine"
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

			expected, err := loadSnapshot(config.BuildSnapshotPath(fileName))
			if err != nil {
				t.Fatalf("failed to load snapshot: %v", err)
			}

			if err := comparePCM(actual, expected, maxAllowedDiff); err != nil {
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

func loadSnapshot(path string) ([]float32, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var pcm []float32
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parsedValue, err := strconv.ParseFloat(line, 32)
		if err != nil {
			return nil, fmt.Errorf("failed to parse float at index %d: %w", len(pcm), err)
		}

		pcm = append(pcm, float32(parsedValue))
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return pcm, nil
}

func comparePCM(actual, expected []float32, maxAbsDiff float32) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("length mismatch: got %d, expected %d", len(actual), len(expected))
	}

	var (
		maxDiff      float32 = 0
		maxDiffIndex int     = -1
	)

	for i := range actual {
		diff := actual[i] - expected[i]
		if diff < 0 {
			diff = -diff
		}
		if diff > maxDiff {
			maxDiff = diff
			maxDiffIndex = i
		}
	}

	if maxDiff > maxAbsDiff {
		return fmt.Errorf("mismatch too high: max diff was %f at index %d (got %f, expected %f, allowed: %f)", maxDiff, maxDiffIndex, actual[maxDiffIndex], expected[maxDiffIndex], maxAbsDiff)
	}

	return nil
}
