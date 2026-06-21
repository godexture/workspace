package id3v2

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

const HeaderSize = 10

var (
	ErrHeaderTooShort = errors.New("id3 header too short")
	ErrInvalidHeader  = errors.New("invalid id3 header")
)

type Header struct {
	VersionMajor    byte
	VersionRevision byte
	Flags           byte
	TagSize         int
}

func ParseHeader(buffer []byte) (Header, error) {
	if len(buffer) < HeaderSize {
		return Header{}, ErrHeaderTooShort
	}
	if !bytes.Equal(buffer[:3], []byte("ID3")) {
		return Header{}, ErrInvalidHeader

	}
	if buffer[3] == 0xFF || buffer[4] == 0xFF {
		return Header{}, ErrInvalidHeader
	}

	switch buffer[3] {
	case byte(Version2), byte(Version3), byte(Version4):
	default:
		return Header{}, ErrInvalidHeader
	}

	if buffer[5]&0x0F != 0 {
		return Header{}, ErrInvalidHeader
	}

	for _, sizeByte := range buffer[6:10] {
		if sizeByte&0x80 != 0 {
			return Header{}, ErrInvalidHeader
		}
	}

	return Header{
		VersionMajor:    buffer[3],
		VersionRevision: buffer[4],
		Flags:           buffer[5],
		TagSize:         DecodeSyncSafeInt(buffer[6:10]),
	}, nil
}

func (h Header) HasFooter() bool {
	return h.VersionMajor == 4 && h.Flags&0x10 != 0
}

func (h Header) TotalSize() int {
	totalSize := HeaderSize + h.TagSize
	if h.HasFooter() {
		totalSize += HeaderSize
	}
	return totalSize
}

func Skip(r io.Reader) (int, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		return 0, errors.New("id3 Skip requires bufio.Reader")
	}

	skippedBytesCount := 0
	for {
		peekedBytes, err := br.Peek(HeaderSize)
		if err != nil {
			if err == io.EOF {
				return skippedBytesCount, nil
			}
			return skippedBytesCount, err
		}

		header, err := ParseHeader(peekedBytes)
		if err != nil {
			if errors.Is(err, ErrInvalidHeader) {
				return skippedBytesCount, nil
			}
			return skippedBytesCount, err
		}

		totalSize := header.TotalSize()
		if _, err := br.Discard(totalSize); err != nil {
			return skippedBytesCount, err
		}
		skippedBytesCount += totalSize
	}
}
