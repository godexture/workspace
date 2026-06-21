package internal

import (
	"bufio"
	"errors"
	"io"

	"github.com/godexture/format-mp3/header"
	"github.com/godexture/metadata-id3/id3v2"
)

var (
	ErrNoSyncWord = errors.New("no mp3 sync word found")
	ErrEOF        = io.EOF
)

type FrameHeader struct {
	Version     int
	Layer       int
	BitRate     int
	SampleRate  int
	Padding     int
	ChannelMode int // header.ChannelModeMono is Mono, others are stereo/dual
	FrameSize   int
	Samples     int
}

// SkipID3v2 skips the ID3v2 tags at the current reader position.
// It returns the number of bytes skipped.
func SkipID3v2(r *bufio.Reader) (int, error) {
	return id3v2.Skip(r)
}

type Header = header.Header

// NextFrameHeader searches for the next sync word and parses the header.
func NextFrameHeader(br *bufio.Reader) (FrameHeader, []byte, error) {
	for {
		// Read 1 byte at a time until we see 0xFF
		currentByte, err := br.ReadByte()
		if err != nil {
			return FrameHeader{}, nil, err
		}

		if currentByte == 0xFF {
			peekedBytes, err := br.Peek(3)
			if err != nil {
				if err == io.EOF {
					return FrameHeader{}, nil, io.EOF
				}
				return FrameHeader{}, nil, err
			}

			// Parse header
			var currentHeader Header
			currentHeader[0] = 0xFF
			copy(currentHeader[1:], peekedBytes[:3])

			if currentHeader.IsValid() {
				// Calculate Frame Size
				frameBytes := currentHeader.FrameBytes(0)
				totalSize := frameBytes + currentHeader.Padding()
				if totalSize <= 4 {
					continue
				}

				// Verify sync word by checking the next frame's header if possible
				verificationBytesCount := totalSize + 3
				nextFramePeekedBytes, err := br.Peek(verificationBytesCount)

				if err == nil {
					var nextHeader Header
					copy(nextHeader[:], nextFramePeekedBytes[totalSize-1:totalSize+3])
					if !currentHeader.Compare(nextHeader) {
						// False sync word! Continue searching.
						continue
					}
				} else {
					_, err2 := br.Peek(totalSize - 1)
					if err2 != nil {
						// Not enough data for a full frame, continue searching
						continue
					}
				}

				// Consume the 3 peeked bytes
				_, _ = br.Discard(3)

				// Read the rest of the frame data
				frameData := make([]byte, totalSize)
				frameData[0] = currentHeader[0]
				frameData[1] = currentHeader[1]
				frameData[2] = currentHeader[2]
				frameData[3] = currentHeader[3]

				_, err = io.ReadFull(br, frameData[4:])
				if err != nil {
					return FrameHeader{}, nil, err
				}

				versionCode := currentHeader.VersionCode()
				layer := currentHeader.Layer()
				sampleRate := currentHeader.SampleRateHz()
				bitrate := currentHeader.BitrateKbps() * 1000

				frameHeader := FrameHeader{
					Version:     versionCode,
					Layer:       layer,
					BitRate:     bitrate,
					SampleRate:  sampleRate,
					Padding:     currentHeader.Padding(),
					ChannelMode: currentHeader.StereoMode(),
					FrameSize:   totalSize,
					Samples:     currentHeader.FrameSamples(),
				}

				return frameHeader, frameData, nil
			}
		}
	}
}
