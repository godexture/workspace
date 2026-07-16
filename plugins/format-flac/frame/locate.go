package frame

import (
	"errors"
	"io"

	"github.com/godexture/format-flac/streaminfo"
)

// LocateFrame returns the first complete, validated FLAC frame at or after the
// reader's current position, along with the scanner positioned after it.
func LocateFrame(r io.ReadSeeker, info streaminfo.StreamInfo) ([]byte, Header, *Scanner, error) {
	base, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, Header{}, nil, err
	}

	readSize := frameReadSize(info.MaxFrameSize)
	buffer := make([]byte, 0, readSize)
	scanPos := 0
	eof := false
	for {
		for scanPos+1 < len(buffer) {
			if buffer[scanPos] != 0xff || buffer[scanPos+1]&0xfc != 0xf8 {
				scanPos++
				continue
			}

			_, parseErr := ParseHeader(buffer[scanPos:], info)
			if errors.Is(parseErr, io.ErrUnexpectedEOF) && !eof {
				break
			}
			if parseErr != nil {
				scanPos++
				continue
			}

			candidate := base + int64(scanPos)
			if _, err := r.Seek(candidate, io.SeekStart); err != nil {
				return nil, Header{}, nil, err
			}
			scanner, err := NewScanner(r, info)
			if err != nil {
				return nil, Header{}, nil, err
			}
			data, header, err := scanner.Next()
			if err == nil {
				return data, header, scanner, nil
			}
			scanPos++
		}

		if eof {
			return nil, Header{}, nil, io.ErrUnexpectedEOF
		}
		if scanPos > 0 {
			keep := len(buffer) - scanPos
			copy(buffer, buffer[scanPos:])
			buffer = buffer[:keep]
			base += int64(scanPos)
			scanPos = 0
		}
		if _, err := r.Seek(base+int64(len(buffer)), io.SeekStart); err != nil {
			return nil, Header{}, nil, err
		}
		oldLen := len(buffer)
		if cap(buffer)-oldLen < readSize {
			grown := make([]byte, oldLen, oldLen+readSize)
			copy(grown, buffer)
			buffer = grown
		}
		buffer = buffer[:cap(buffer)]
		n, readErr := r.Read(buffer[oldLen:])
		buffer = buffer[:oldLen+n]
		if readErr == io.EOF {
			eof = true
		} else if readErr != nil {
			return nil, Header{}, nil, readErr
		} else if n == 0 {
			return nil, Header{}, nil, io.ErrNoProgress
		}
	}
}
