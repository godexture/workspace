package internal

import (
	"bufio"
	"io"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugin/mp3/header"
	"github.com/godexture/godec/plugin/mp3/scan"
	"github.com/godexture/godec/plugin/id3/id3v2"
)

// scanWindowBytes is how much unread data nextFrame peeks at a time while
// resynchronizing. It must exceed scan.MaxLookback so every candidate
// evaluated within a window is either matched or conclusively rejected
// before the window is advanced.
const scanWindowBytes = 8192

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

func nextFrameHeader(br *bufio.Reader, freeFormatBytes int) (FrameHeader, []byte, int, error) {
	return nextFrame(br, freeFormatBytes,
		func(size int) ([]byte, []byte) {
			data := make([]byte, size)
			return data, data
		},
		func([]byte) {},
	)
}

func nextFramePacket(br *bufio.Reader, freeFormatBytes int) (FrameHeader, *media.Packet, int, error) {
	return nextFrame(br, freeFormatBytes,
		func(size int) (*media.Packet, []byte) {
			packet := media.NewPacket(size)
			return packet, packet.Data()
		},
		func(packet *media.Packet) { packet.Release() },
	)
}

// readFramePacket reads the frame that starts at the reader's current
// position without resynchronizing, trusting that the caller is already
// frame-aligned (e.g. right after a previously decoded frame).
func readFramePacket(br *bufio.Reader, freeFormatBytes int) (FrameHeader, *media.Packet, bool, error) {
	headerBytes, err := br.Peek(4)
	if err != nil {
		return FrameHeader{}, nil, false, err
	}
	var currentHeader Header
	copy(currentHeader[:], headerBytes)
	if !currentHeader.IsValid() {
		return FrameHeader{}, nil, false, nil
	}
	totalSize := currentHeader.FrameBytes(freeFormatBytes) + currentHeader.Padding()
	if totalSize <= 4 {
		return FrameHeader{}, nil, false, nil
	}
	if _, err := br.Peek(totalSize); err != nil {
		return FrameHeader{}, nil, false, nil
	}
	packet := media.NewPacket(totalSize)
	if _, err := io.ReadFull(br, packet.Data()); err != nil {
		packet.Release()
		return FrameHeader{}, nil, false, err
	}
	return makeFrameHeader(currentHeader, totalSize), packet, true, nil
}

// nextFrame resynchronizes by scanning forward for the next valid frame,
// verifying it against a following frame header before accepting it.
//
// freeFormatBytes carries a frame size previously resolved for a
// free-format stream (0 if unknown); nextFrame returns the value to thread
// into the next call.
func nextFrame[T any](
	br *bufio.Reader,
	freeFormatBytes int,
	allocate func(int) (T, []byte),
	release func(T),
) (FrameHeader, T, int, error) {
	var zero T

	for {
		window, peekErr := br.Peek(scanWindowBytes)
		if len(window) == 0 {
			if peekErr != nil {
				return FrameHeader{}, zero, freeFormatBytes, peekErr
			}
			return FrameHeader{}, zero, freeFormatBytes, io.EOF
		}

		offset, frameSize, newFreeFormatBytes, found := scan.Frame(window, freeFormatBytes)
		if found {
			var currentHeader Header
			copy(currentHeader[:], window[offset:offset+4])

			if _, err := br.Discard(offset); err != nil {
				return FrameHeader{}, zero, freeFormatBytes, err
			}

			result, frameData := allocate(frameSize)
			if _, err := io.ReadFull(br, frameData); err != nil {
				release(result)
				return FrameHeader{}, zero, freeFormatBytes, err
			}

			return makeFrameHeader(currentHeader, frameSize), result, newFreeFormatBytes, nil
		}

		if peekErr != nil {
			// The window already holds everything left in the stream and
			// still contains no frame.
			if _, err := br.Discard(len(window)); err != nil {
				return FrameHeader{}, zero, freeFormatBytes, err
			}
			return FrameHeader{}, zero, freeFormatBytes, io.EOF
		}

		// The window was inconclusive but full. Everything before the last
		// scan.MaxLookback bytes had enough trailing context for Frame to
		// reach a final verdict, so it's safe to drop and retry with more
		// data appended past it.
		if _, err := br.Discard(len(window) - scan.MaxLookback); err != nil {
			return FrameHeader{}, zero, freeFormatBytes, err
		}
	}
}

func makeFrameHeader(header Header, frameSize int) FrameHeader {
	return FrameHeader{
		Version:     header.VersionCode(),
		Layer:       header.Layer(),
		BitRate:     header.BitrateKbps() * 1000,
		SampleRate:  header.SampleRateHz(),
		Padding:     header.Padding(),
		ChannelMode: header.StereoMode(),
		FrameSize:   frameSize,
		Samples:     header.FrameSamples(),
	}
}
