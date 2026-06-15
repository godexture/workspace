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

const maxAllowedDiff = 40

func TestSnapshots(t *testing.T) {
	for _, filename := range testFiles {
		t.Run(filename, func(t *testing.T) {
			mp3Path := filepath.Join("testdata", filename)
			mp3Data, err := os.ReadFile(mp3Path)
			if err != nil {
				t.Fatalf("failed to read test MP3 file: %v", err)
			}

			// Decode using Demuxer and Decoder
			pcm, err := decodeAll(mp3Data)
			if err != nil {
				t.Fatalf("failed to decode MP3 data: %v", err)
			}

			snapshotDir := filepath.Join("testdata", "snapshots")
			snapshotPath := filepath.Join(snapshotDir, filename+".snapshot")

			// Load snapshot and compare
			expectedPcm, err := loadSnapshot(snapshotPath)
			if err != nil {
				t.Fatalf("failed to load snapshot: %v", err)
			}

			if err := comparePCM(pcm, expectedPcm, maxAllowedDiff); err != nil {
				t.Errorf("PCM comparison failed: %v", err)
			}
		})
	}
}

func decodeAll(mp3Data []byte) ([]int16, error) {
	demux, err := mp3format.NewDemuxerEngine(bytes.NewReader(mp3Data))
	if err != nil {
		return nil, err
	}

	_, _, err = demux.Analyze()
	if err != nil {
		return nil, err
	}

	dec := mp3codec.NewDecoderEngine(mp3codec.DecoderConfig{})

	var allPCM []int16

	for {
		pkt, _, err := demux.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if err := dec.SendPacket(pkt); err != nil {
			if err != engine.ErrEAGAIN {
				return nil, err
			}
		}

		for {
			frame, err := dec.ReceiveFrame()
			if err == engine.ErrEAGAIN {
				break
			}
			if err != nil {
				return nil, err
			}

			audioFrame, ok := (*frame).(*media.AudioFrame)
			if !ok {
				return nil, fmt.Errorf("expected AudioFrame")
			}

			plane := audioFrame.Planes()[0]
			channels := audioFrame.Layout.ChannelCount()
			samples := audioFrame.Samples
			totalSamples := samples * channels

			for i := 0; i < totalSamples; i++ {
				valInt := int16(binary.LittleEndian.Uint16(plane[i*2 : i*2+2]))
				allPCM = append(allPCM, valInt)
			}
		}
	}

	// Flush
	if err := dec.Flush(); err != nil {
		return nil, err
	}

	for {
		frame, err := dec.ReceiveFrame()
		if err == engine.ErrEOF || err == engine.ErrEAGAIN {
			break
		}
		if err != nil {
			return nil, err
		}

		audioFrame, ok := (*frame).(*media.AudioFrame)
		if !ok {
			return nil, fmt.Errorf("expected AudioFrame")
		}

		plane := audioFrame.Planes()[0]
		channels := audioFrame.Layout.ChannelCount()
		samples := audioFrame.Samples
		totalSamples := samples * channels

		for i := 0; i < totalSamples; i++ {
			valInt := int16(binary.LittleEndian.Uint16(plane[i*2 : i*2+2]))
			allPCM = append(allPCM, valInt)
		}
	}

	return allPCM, nil
}

func loadSnapshot(path string) ([]int16, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var data []int16
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		val, err := strconv.ParseInt(line, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("failed to parse int at index %d: %w", len(data), err)
		}
		data = append(data, int16(val))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return data, nil
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
