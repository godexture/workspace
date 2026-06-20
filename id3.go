package id3

import (
	"bufio"
	"bytes"
	"errors"
	"io"

	"github.com/godexture/core/domain/metadata"
)

const HeaderSize = 10
const V1TagSize = 128

var (
	ErrHeaderTooShort = errors.New("id3 header too short")
	ErrInvalidHeader  = errors.New("invalid id3 header")
)

const commentLanguage = "eng"

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
		TagSize:         decodeSyncSafeInt(buffer[6:10]),
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

func SkipID3v2(r io.Reader) (int, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		return 0, errors.New("id3 SkipID3v2 requires bufio.Reader")
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

func HasV1Tag(buffer []byte) bool {
	return len(buffer) >= V1TagSize && bytes.Equal(buffer[len(buffer)-V1TagSize:len(buffer)-V1TagSize+3], []byte("TAG"))
}

func TrimID3(buffer []byte) (start int, end int) {
	start = 0
	for len(buffer[start:]) >= HeaderSize {
		header, err := ParseHeader(buffer[start:])
		if err != nil {
			break
		}
		start += header.TotalSize()
		if start >= len(buffer) {
			return len(buffer), len(buffer)
		}
	}

	end = len(buffer)
	if HasV1Tag(buffer[:end]) {
		end -= V1TagSize
	}
	if end < start {
		end = start
	}
	return start, end
}

func Parse(buffer []byte) (*metadata.Bundle, error) {
	bundle := metadata.NewBundle()
	parseLeadingV2(buffer, bundle)
	parseTrailingV1(buffer, bundle)
	return bundle, nil
}

type Version byte

const (
	Version2v2 Version = 2
	Version2v3 Version = 3
	Version2v4 Version = 4
)

type Encoding byte

const (
	EncodingDefault  Encoding = 0x00
	EncodingISO88591 Encoding = 0x01
	EncodingUTF16    Encoding = 0x02
	EncodingUTF16BE  Encoding = 0x03
	EncodingUTF8     Encoding = 0x04
)

type MarshalOptions struct {
	Version  Version
	Encoding Encoding
}
