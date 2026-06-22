package id3

import (
	"io"

	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/metadata-id3/id3v1"
	"github.com/godexture/metadata-id3/id3v2"
)

func Trim(buffer []byte) (start int, end int) {
	start = 0
	for len(buffer[start:]) >= id3v2.HeaderSize {
		header, err := id3v2.ParseHeader(buffer[start:])
		if err != nil {
			break
		}
		start += header.TotalSize()
		if start >= len(buffer) {
			return len(buffer), len(buffer)
		}
	}

	end = len(buffer)
	if id3v1.HasTag(buffer[:end]) {
		end -= id3v1.TagSize
	}
	if end < start {
		end = start
	}
	return start, end
}

func Parse(buffer []byte) (*metadata.Bundle, error) {
	bundle := metadata.NewBundle()
	id3v1.Parse(buffer, bundle)

	v2Bundle := metadata.NewBundle()
	id3v2.Parse(buffer, v2Bundle)
	bundle.Merge(v2Bundle)

	return bundle, nil
}

// ParseReader parses ID3v2 and ID3v1 tags from an io.ReadSeeker.
func ParseReader(r io.ReadSeeker) (*metadata.Bundle, error) {
	bundle := metadata.NewBundle()
	v2Bundle := metadata.NewBundle()

	if _, err := r.Seek(0, io.SeekStart); err == nil {
		fileSize, sizeErr := r.Seek(0, io.SeekEnd)
		if sizeErr != nil {
			fileSize = 0
		}
		_, _ = r.Seek(0, io.SeekStart)

		var totalID3v2Size int64
		for {
			headerBytes := make([]byte, id3v2.HeaderSize)
			if _, err := io.ReadFull(r, headerBytes); err != nil {
				break
			}
			header, err := id3v2.ParseHeader(headerBytes)
			if err != nil {
				break
			}
			nextSize := totalID3v2Size + int64(header.TotalSize())
			if fileSize > 0 && nextSize > fileSize {
				break
			}
			totalID3v2Size = nextSize
			if _, err := r.Seek(totalID3v2Size, io.SeekStart); err != nil {
				break
			}
		}

		if totalID3v2Size > 0 {
			if _, err := r.Seek(0, io.SeekStart); err == nil {
				id3v2Buf := make([]byte, int(totalID3v2Size))
				if _, err := io.ReadFull(r, id3v2Buf); err == nil {
					id3v2.Parse(id3v2Buf, v2Bundle)
				}
			}
		}
	}

	if _, err := r.Seek(-int64(id3v1.TagSize), io.SeekEnd); err == nil {
		id3v1Buf := make([]byte, id3v1.TagSize)
		if _, err := io.ReadFull(r, id3v1Buf); err == nil {
			id3v1.Parse(id3v1Buf, bundle)
		}
	}

	bundle.Merge(v2Bundle)

	return bundle, nil
}
