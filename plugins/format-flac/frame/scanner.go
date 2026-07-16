package frame

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/hash"
)

const (
	minReadSize         = 64 << 10
	maxReadSize         = 1 << 20
	defaultMaxFrameSize = 1 << 24
)

// Scanner splits native FLAC audio data into complete, validated frames.
type Scanner struct {
	r            io.Reader
	info         streaminfo.StreamInfo
	readSize     int
	maxFrameSize int
	buffer       []byte
	eof          bool
}

func NewScanner(r io.Reader, info streaminfo.StreamInfo) (*Scanner, error) {
	if r == nil {
		return nil, errors.New("FLAC frame scanner requires a non-nil reader")
	}
	readSize := frameReadSize(info.MaxFrameSize)
	// STREAMINFO frame-size fields are advisory and are frequently absent or
	// stale in real-world files. Keep the scanner bounded by the format limit.
	maxFrameSize := defaultMaxFrameSize
	return &Scanner{r: r, info: info, readSize: readSize, maxFrameSize: maxFrameSize}, nil
}

func frameReadSize(maxFrameSize uint32) int {
	size := uint32(minReadSize)
	if maxFrameSize > size {
		size = maxFrameSize
	}
	if size >= maxReadSize {
		return maxReadSize
	}
	size--
	size |= size >> 1
	size |= size >> 2
	size |= size >> 4
	size |= size >> 8
	size |= size >> 16
	return int(size + 1)
}

func (s *Scanner) Next() ([]byte, Header, error) {
	if err := s.ensureHeader(); err != nil {
		return nil, Header{}, err
	}
	current, err := ParseHeader(s.buffer, s.info)
	if err != nil {
		return nil, Header{}, fmt.Errorf("parse FLAC frame header: %w", err)
	}
	for {
		if boundary, next, ok := s.findBoundary(current); ok {
			data := append([]byte(nil), s.buffer[:boundary]...)
			s.buffer = append(s.buffer[:0], s.buffer[boundary:]...)
			current.FrameBytes = len(data)
			_ = next
			return data, current, nil
		}
		if s.eof {
			if len(s.buffer) < 3 {
				return nil, Header{}, io.ErrUnexpectedEOF
			}
			footer := uint16(s.buffer[len(s.buffer)-2])<<8 | uint16(s.buffer[len(s.buffer)-1])
			if hash.CRC16(s.buffer[:len(s.buffer)-2]) != footer {
				return nil, Header{}, errors.New("invalid FLAC frame footer CRC-16")
			}
			data := append([]byte(nil), s.buffer...)
			s.buffer = nil
			current.FrameBytes = len(data)
			return data, current, nil
		}
		if len(s.buffer) > s.maxFrameSize {
			return nil, Header{}, fmt.Errorf("FLAC frame exceeds maximum size %d without a valid boundary", s.maxFrameSize)
		}
		if err := s.readMore(); err != nil {
			return nil, Header{}, err
		}
	}
}

func (s *Scanner) ensureHeader() error {
	for len(s.buffer) == 0 && !s.eof {
		if err := s.readMore(); err != nil {
			return err
		}
	}
	if len(s.buffer) == 0 {
		return io.EOF
	}
	for {
		_, err := ParseHeader(s.buffer, s.info)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		if s.eof {
			return err
		}
		if err := s.readMore(); err != nil {
			return err
		}
	}
}

func (s *Scanner) findBoundary(current Header) (int, Header, bool) {
	for pos := 2; pos+2 <= len(s.buffer); pos++ {
		if s.buffer[pos] != 0xff || s.buffer[pos+1]&0xfc != 0xf8 {
			continue
		}
		next, err := ParseHeader(s.buffer[pos:], s.info)
		if err != nil {
			continue
		}
		if !continuous(current, next) || pos < 2 {
			continue
		}
		footer := uint16(s.buffer[pos-2])<<8 | uint16(s.buffer[pos-1])
		if hash.CRC16(s.buffer[:pos-2]) != footer {
			continue
		}
		return pos, next, true
	}
	return 0, Header{}, false
}

func continuous(current, next Header) bool {
	if current.BlockingStrategy != next.BlockingStrategy {
		return false
	}
	// Older encoders have emitted block-numbering combinations that do not
	// match the blocking-strategy flag. Header and frame CRCs provide the
	// boundary proof; retain monotonicity for those streams.
	return next.Number > current.Number
}

func (s *Scanner) readMore() error {
	buf := make([]byte, s.readSize)
	n, err := s.r.Read(buf)
	if n > 0 {
		s.buffer = append(s.buffer, buf[:n]...)
	}
	if err == io.EOF {
		s.eof = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("read FLAC audio frames: %w", err)
	}
	if n == 0 {
		return io.ErrNoProgress
	}
	return nil
}
