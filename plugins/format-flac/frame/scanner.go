package frame

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/godec/plugins/format-flac/streaminfo"
	"github.com/godexture/godec/sdk/buffer"
	"github.com/godexture/godec/sdk/hash"
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
	buffer       buffer.Ring[byte]
	eof          bool
	scanPos      int
	crc          uint16
	crcPos       int
	options      Options
	resyncPos    int
	offset       int64
	frameOffset  int64
}

type Options struct {
	Strict bool
	Sync   bool
}

func NewScanner(r io.Reader, info streaminfo.StreamInfo, options Options) (*Scanner, error) {
	if r == nil {
		return nil, errors.New("FLAC frame scanner requires a non-nil reader")
	}
	readSize := frameReadSize(info.MaxFrameSize)
	// STREAMINFO frame-size fields are advisory and are frequently absent or
	// stale in real-world files. Keep the scanner bounded by the format limit.
	maxFrameSize := defaultMaxFrameSize
	return &Scanner{
		r:            r,
		info:         info,
		readSize:     readSize,
		maxFrameSize: maxFrameSize,
		buffer:       buffer.NewRing[byte](readSize),
		scanPos:      2,
		options:      options,
		resyncPos:    -1,
	}, nil
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

func (s *Scanner) extractFrame(boundary int, hdr Header) ([]byte, Header, error) {
	data := append([]byte(nil), s.buffer.Data()[:boundary]...)
	s.buffer.Discard(boundary)
	s.offset += int64(boundary)
	s.scanPos = 2
	s.crc = 0
	s.crcPos = 0
	s.resyncPos = -1
	hdr.FrameBytes = len(data)
	return data, hdr, nil
}

func (s *Scanner) Next() ([]byte, Header, error) {
	if err := s.ensureHeader(); err != nil {
		return nil, Header{}, err
	}
	current, err := s.currentHeader()
	if err != nil {
		return nil, Header{}, err
	}
	for {
		if boundary, ok := s.findBoundary(current); ok {
			return s.extractFrame(boundary, current)
		}
		if s.eof {
			data := s.buffer.Data()
			if len(data) < 3 {
				return nil, Header{}, io.ErrUnexpectedEOF
			}
			footer := uint16(data[len(data)-2])<<8 | uint16(data[len(data)-1])
			if hash.CRC16(data[:len(data)-2]) != footer {
				if s.options.Strict {
					return nil, Header{}, errors.New("invalid FLAC frame footer CRC-16")
				}
				if s.resyncPos > 0 {
					s.discard(s.resyncPos)
					return s.Next()
				}
				return nil, Header{}, io.EOF
			}
			return s.extractFrame(len(data), current)
		}
		if s.buffer.Len() > s.maxFrameSize {
			if s.options.Strict {
				return nil, Header{}, fmt.Errorf("FLAC frame exceeds maximum size %d without a valid boundary", s.maxFrameSize)
			}
			if s.resyncPos > 0 {
				s.discard(s.resyncPos)
			} else {
				s.discard(1)
			}
			return s.Next()
		}
		if err := s.readMore(); err != nil {
			return nil, Header{}, err
		}
	}
}

func (s *Scanner) currentHeader() (Header, error) {
	for {
		header, err := ParseHeader(s.buffer.Data(), s.info)
		if err == nil {
			s.frameOffset = s.offset
			return header, nil
		}
		if s.options.Strict && !s.options.Sync {
			return Header{}, fmt.Errorf("parse FLAC frame header: %w", err)
		}
		if errors.Is(err, io.ErrUnexpectedEOF) && !s.eof {
			if err := s.readMore(); err != nil {
				return Header{}, err
			}
			continue
		}
		if !s.options.Sync && s.options.Strict {
			return Header{}, fmt.Errorf("parse FLAC frame header: %w", err)
		}
		if !s.findNextHeader(1) {
			if s.eof {
				return Header{}, io.EOF
			}
			if err := s.readMore(); err != nil {
				return Header{}, err
			}
			continue
		}
	}
}

func (s *Scanner) FrameOffset() int64 { return s.frameOffset }

func (s *Scanner) findNextHeader(start int) bool {
	data := s.buffer.Data()
	for pos := nextSyncPrefix(data, start); pos >= 0; pos = nextSyncPrefix(data, pos+1) {
		if _, err := ParseHeader(data[pos:], s.info); err == nil {
			s.discard(pos)
			return true
		}
	}
	if len(data) > 1 {
		s.discard(len(data) - 1)
	}
	return false
}

func (s *Scanner) discard(n int) {
	if n <= 0 {
		return
	}
	s.buffer.Discard(n)
	s.offset += int64(n)
	s.scanPos, s.crc, s.crcPos, s.resyncPos = 2, 0, 0, -1
}

func (s *Scanner) ensureHeader() error {
	for s.buffer.Len() == 0 && !s.eof {
		if err := s.readMore(); err != nil {
			return err
		}
	}
	if s.buffer.Len() == 0 {
		return io.EOF
	}
	return nil
}

func (s *Scanner) findBoundary(current Header) (int, bool) {
	data := s.buffer.Data()
	for pos := nextSyncPrefix(data, s.scanPos); pos >= 0; pos = nextSyncPrefix(data, pos+1) {
		next, err := ParseHeader(data[pos:], s.info)
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				s.scanPos = pos
				return 0, false
			}
			continue
		}
		if !continuous(current, next) || pos < 2 {
			continue
		}
		footer := uint16(data[pos-2])<<8 | uint16(data[pos-1])
		if s.crcPos < pos-2 {
			s.crc = hash.CRC16Update(s.crc, data[s.crcPos:pos-2])
			s.crcPos = pos - 2
		}
		if s.crc != footer {
			if !s.options.Strict && s.resyncPos < 0 {
				s.resyncPos = pos
			}
			continue
		}
		return pos, true
	}
	s.scanPos = max(2, len(data)-1)
	return 0, false
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
	buffered := s.buffer.Len()
	growSize := s.readSize
	if available := s.buffer.Cap() - buffered; available > 0 && available < growSize {
		growSize = available
	}
	n, err := s.r.Read(s.buffer.Grow(growSize))
	s.buffer.Truncate(buffered + n)

	switch {
	case err == io.EOF:
		s.eof = true
		return nil
	case err != nil:
		return fmt.Errorf("read FLAC audio frames: %w", err)
	case n == 0:
		return io.ErrNoProgress
	}
	return nil
}
