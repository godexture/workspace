package test

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	mp3codec "github.com/godexture/codec-mp3"
	"github.com/godexture/core/domain/media"
	mp3format "github.com/godexture/format-mp3"
	"github.com/godexture/sdk/engine"
)

var testFiles = []string{
	"l1-fl4.mp3",
	"l2-fl13.mp3",
	"l3-he_32khz.mp3",
	"l3-hecommon.mp3",
	"l3-nonstandard-id3v2.mp3",
	"l3-sin1k0db.mp3",
}

const maxAllowedDiff = 1

func TestSnapshots(t *testing.T) {
	for _, fileName := range testFiles {
		t.Run(fileName, func(t *testing.T) {
			mp3Path := filepath.Join("testdata", fileName)
			mp3Data, err := os.ReadFile(mp3Path)
			if err != nil {
				t.Fatalf("failed to read test MP3 file: %v", err)
			}

			// Decode using Demuxer and Decoder
			pcmSamples, err := decodeAll(mp3Data)
			if err != nil {
				t.Fatalf("failed to decode MP3 data: %v", err)
			}

			snapshotDirectory := filepath.Join("testdata", "snapshots")
			snapshotPath := filepath.Join(snapshotDirectory, fileName+".snapshot")

			// Load snapshot and compare
			expectedPcm, err := loadSnapshot(snapshotPath)
			if err != nil {
				t.Fatalf("failed to load snapshot: %v", err)
			}

			if err := comparePCM(pcmSamples, expectedPcm, maxAllowedDiff); err != nil {
				t.Errorf("PCM comparison failed: %v", err)
			}
		})
	}
}

func decodeAll(mp3Data []byte) ([]int16, error) {
	demuxer, err := mp3format.NewDemuxerEngine(bytes.NewReader(mp3Data))
	if err != nil {
		return nil, err
	}

	_, _, err = demuxer.Analyze()
	if err != nil {
		return nil, err
	}

	decoder := mp3codec.NewDecoderEngine(mp3codec.DecoderConfig{})

	var allPcmSamples []int16

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
				valueInteger := int16(binary.LittleEndian.Uint16(plane[i*2 : i*2+2]))
				allPcmSamples = append(allPcmSamples, valueInteger)
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
			valueInteger := int16(binary.LittleEndian.Uint16(plane[i*2 : i*2+2]))
			allPcmSamples = append(allPcmSamples, valueInteger)
		}
	}

	return allPcmSamples, nil
}

func loadSnapshot(path string) ([]int16, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var pcmData []int16
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parsedValue, err := strconv.ParseInt(line, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("failed to parse int at index %d: %w", len(pcmData), err)
		}
		pcmData = append(pcmData, int16(parsedValue))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return pcmData, nil
}

func comparePCM(actual, expected []int16, maxAbsDiff int) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("length mismatch: got %d, expected %d", len(actual), len(expected))
	}

	maxDiff := 0
	maxDiffIdx := 0

	for i := range actual {
		diff := int(actual[i]) - int(expected[i])
		if diff < 0 {
			diff = -diff
		}
		if diff > maxDiff {
			maxDiff = diff
			maxDiffIdx = i
		}
	}

	if maxDiff > maxAbsDiff {
		return fmt.Errorf("mismatch too high: max diff was %d at index %d (got %d, expected %d, allowed: %d)", maxDiff, maxDiffIdx, actual[maxDiffIdx], expected[maxDiffIdx], maxAbsDiff)
	}
	return nil
}
