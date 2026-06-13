package internal

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
)

var (
	ErrNoSyncWord = errors.New("no mp3 sync word found")
	ErrEOF        = io.EOF
)

// MPEG Version
const (
	MPEG2_5 = 0
	MPEGReserved = 1
	MPEG2 = 2
	MPEG1 = 3
)

// MPEG Layer
const (
	LayerReserved = 0
	Layer3 = 1
	Layer2 = 2
	Layer1 = 3
)

var bitratesMPEG1Layer3 = []int{
	0, 32000, 40000, 48000, 56000, 64000, 80000, 96000,
	112000, 128000, 160000, 192000, 224000, 256000, 320000, -1,
}

var bitratesMPEG2Layer3 = []int{
	0, 8000, 16000, 24000, 32000, 40000, 48000, 56000,
	64000, 80000, 96000, 112000, 128000, 144000, 160000, -1,
}

var sampleRatesMPEG1 = []int{44100, 48000, 32000, -1}
var sampleRatesMPEG2 = []int{22050, 24000, 16000, -1}
var sampleRatesMPEG2_5 = []int{11025, 12000, 8000, -1}

type FrameHeader struct {
	Version    int
	Layer      int
	BitRate    int
	SampleRate int
	Padding    int
	ChannelMode int // 3 is Mono, others are stereo/dual
	FrameSize  int
}

// SkipID3v2 skips the ID3v2 tags at the current reader position.
// It returns the number of bytes skipped.
func SkipID3v2(r io.Reader) (int, error) {
	// Actually we should use a bufferedReader to peek, but for simplicity,
	// let's assume the caller uses a bufio.Reader.
	br, ok := r.(*bufio.Reader)
	if !ok {
		return 0, errors.New("SkipID3v2 requires bufio.Reader")
	}

	skipped := 0
	for {
		peek, err := br.Peek(10)
		if err != nil {
			if err == io.EOF {
				break
			}
			return skipped, err
		}
		if bytes.HasPrefix(peek, []byte("ID3")) {
			// Parse size
			size := (int(peek[6]) << 21) | (int(peek[7]) << 14) | (int(peek[8]) << 7) | int(peek[9])
			total := size + 10
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

// NextFrameHeader searches for the next sync word and parses the header.
func NextFrameHeader(br *bufio.Reader) (FrameHeader, []byte, error) {
	var header FrameHeader
	for {
		// Read 1 byte at a time until we see 0xFF
		b, err := br.ReadByte()
		if err != nil {
			return header, nil, err
		}

		if b == 0xFF {
			peek, err := br.Peek(3)
			if err != nil {
				if err == io.EOF {
					return header, nil, io.EOF
				}
				return header, nil, err
			}

			// Check sync word
			if (peek[0] & 0xE0) == 0xE0 { // 111
				// Valid sync word found. We have 4 bytes: 0xFF and peek[0], peek[1], peek[2]
				b1 := peek[0]
				b2 := peek[1]
				b3 := peek[2]

				version := (int(b1) >> 3) & 0x03
				layer := (int(b1) >> 1) & 0x03
				// Ignore CRC protection bit (b1 & 0x01)
				
				bitrateIdx := (int(b2) >> 4) & 0x0F
				sampleRateIdx := (int(b2) >> 2) & 0x03
				paddingBit := (int(b2) >> 1) & 0x01
				
				channelMode := (int(b3) >> 6) & 0x03

				if version == MPEGReserved || layer == LayerReserved || bitrateIdx == 0x0F || sampleRateIdx == 0x03 {
					// Invalid header, continue searching
					continue
				}

				// Calculate Bitrate
				var bitrate int
				if layer == Layer3 {
					if version == MPEG1 {
						bitrate = bitratesMPEG1Layer3[bitrateIdx]
					} else {
						bitrate = bitratesMPEG2Layer3[bitrateIdx]
					}
				} else {
					// We only fully support Layer3 bitrate parsing here, but can expand if needed.
					// If not layer3, just let's skip for simplicity or return an error.
					return header, nil, fmt.Errorf("unsupported mpeg layer %d", layer)
				}

				if bitrate <= 0 {
					continue // free bitrate not supported
				}

				// Calculate SampleRate
				var sampleRate int
				switch version {
				case MPEG1:
					sampleRate = sampleRatesMPEG1[sampleRateIdx]
				case MPEG2:
					sampleRate = sampleRatesMPEG2[sampleRateIdx]
				case MPEG2_5:
					sampleRate = sampleRatesMPEG2_5[sampleRateIdx]
				}

				if sampleRate <= 0 {
					continue
				}

				// Calculate Frame Size
				var frameSize int
				if version == MPEG1 {
					frameSize = 144 * bitrate / sampleRate + paddingBit
				} else {
					// MPEG2/2.5 Layer 3
					frameSize = 72 * bitrate / sampleRate + paddingBit
				}

				header = FrameHeader{
					Version:    version,
					Layer:      layer,
					BitRate:    bitrate,
					SampleRate: sampleRate,
					Padding:    paddingBit,
					ChannelMode: channelMode,
					FrameSize:  frameSize,
				}

				// Consume the 3 peeked bytes
				_, _ = br.Discard(3)

				// Read the rest of the frame data
				frameData := make([]byte, frameSize)
				frameData[0] = 0xFF
				frameData[1] = b1
				frameData[2] = b2
				frameData[3] = b3

				_, err = io.ReadFull(br, frameData[4:])
				if err != nil {
					return header, nil, err
				}

				return header, frameData, nil
			}
		}
	}
}
