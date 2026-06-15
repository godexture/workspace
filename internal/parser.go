package internal

import (
	"bufio"
	"bytes"
	"errors"
	"io"

	"github.com/godexture/format-mp3/header"
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
func SkipID3v2(r io.Reader) (int, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		return 0, errors.New("SkipID3v2 requires bufio.Reader")
	}

	skipped := 0
	for {
		peek, err := br.Peek(header.ID3v2HeaderSize)
		if err != nil {
			if err == io.EOF {
				break
			}
			return skipped, err
		}
		if bytes.HasPrefix(peek, []byte("ID3")) {
			// Parse size
			size := (int(peek[6]) << 21) | (int(peek[7]) << 14) | (int(peek[8]) << 7) | int(peek[9])
			total := size + header.ID3v2HeaderSize
			// Skip the bytes
			_, err = br.Discard(total)
			if err != nil {
				return skipped, err
			}
			skipped += total
		} else {
			break
		}
	}
	return skipped, nil
}

type Header = header.Header

// NextFrameHeader searches for the next sync word and parses the header.
func NextFrameHeader(br *bufio.Reader) (FrameHeader, []byte, error) {
	for {
		// Read 1 byte at a time until we see 0xFF
		b, err := br.ReadByte()
		if err != nil {
			return FrameHeader{}, nil, err
		}

		if b == 0xFF {
			peek, err := br.Peek(3)
			if err != nil {
				if err == io.EOF {
					return FrameHeader{}, nil, io.EOF
				}
				return FrameHeader{}, nil, err
			}

			// Parse header
			var h Header
			h[0] = 0xFF
			copy(h[1:], peek[:3])

			if h.IsValid() {
				// Calculate Frame Size
				frameBytes := h.FrameBytes(0)
				totalSize := frameBytes + h.Padding()
				if totalSize <= 4 {
					continue
				}

				// Verify sync word by checking the next frame's header if possible
				verifyBytes := totalSize + 3
				nextPeek, peekErr := br.Peek(verifyBytes)

				if peekErr == nil {
					var nextHdr Header
					copy(nextHdr[:], nextPeek[totalSize-1:totalSize+3])
					if !h.Compare(nextHdr) {
						// False sync word! Continue searching.
						continue
					}
				} else {
					_, peekErr2 := br.Peek(totalSize - 1)
					if peekErr2 != nil {
						// Not enough data for a full frame, continue searching
						continue
					}
				}

				// Consume the 3 peeked bytes
				_, _ = br.Discard(3)

				// Read the rest of the frame data
				frameData := make([]byte, totalSize)
				frameData[0] = h[0]
				frameData[1] = h[1]
				frameData[2] = h[2]
				frameData[3] = h[3]

				_, err = io.ReadFull(br, frameData[4:])
				if err != nil {
					return FrameHeader{}, nil, err
				}

				versionCode := h.VersionCode()
				layer := h.Layer()
				sampleRate := h.SampleRateHz()
				bitrate := h.BitrateKbps() * 1000

				fh := FrameHeader{
					Version:     versionCode,
					Layer:       layer,
					BitRate:     bitrate,
					SampleRate:  sampleRate,
					Padding:     h.Padding(),
					ChannelMode: h.StereoMode(),
					FrameSize:   totalSize,
					Samples:     h.FrameSamples(),
				}

				return fh, frameData, nil
			}
		}
	}
}
