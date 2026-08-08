package internal

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/godec/plugin/flac/internal/seektable"
	"github.com/godexture/godec/plugin/flac/internal/streaminfo"
	"github.com/godexture/godec/plugin/id3/id3v2"
)

func parseNativeFLACHeader(r io.ReadSeeker, strict bool) (streaminfo.StreamInfo, []byte, [][]byte, []seektable.Point, int64, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return streaminfo.StreamInfo{}, nil, nil, nil, 0, err
	}

	if !strict {
		for {
			position, err := r.Seek(0, io.SeekCurrent)
			if err != nil {
				return streaminfo.StreamInfo{}, nil, nil, nil, 0, err
			}
			var id3Header [id3v2.HeaderSize]byte
			if _, err := io.ReadFull(r, id3Header[:]); err != nil {
				break
			}
			header, err := id3v2.ParseHeader(id3Header[:])
			if err != nil {
				_, _ = r.Seek(position, io.SeekStart)
				break
			}
			if _, err := r.Seek(position+int64(header.TotalSize()), io.SeekStart); err != nil {
				return streaminfo.StreamInfo{}, nil, nil, nil, 0, err
			}
		}
	}
	var marker [4]byte
	if _, err := io.ReadFull(r, marker[:]); err != nil {
		return streaminfo.StreamInfo{}, nil, nil, nil, 0, fmt.Errorf("read FLAC marker: %w", err)
	}
	if string(marker[:]) != streaminfo.Marker {
		return streaminfo.StreamInfo{}, nil, nil, nil, 0, errors.New("not a native FLAC stream")
	}

	seenStreamInfo := false
	var parsedInfo streaminfo.StreamInfo
	var streamInfoBlock []byte
	var extraBlocks [][]byte
	var seekPoints []seektable.Point
	for {
		var header [4]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return streaminfo.StreamInfo{}, nil, nil, nil, 0, fmt.Errorf("read FLAC metadata header: %w", err)
		}

		isLast, blockType, length := streaminfo.ParseBlockHeader(header)
		if length < 0 || length > (1<<24)-1 {
			return streaminfo.StreamInfo{}, nil, nil, nil, 0, errors.New("invalid FLAC metadata length")
		}
		if strict && blockType > 6 {
			return streaminfo.StreamInfo{}, nil, nil, nil, 0, fmt.Errorf("reserved FLAC metadata block type: %d", blockType)
		}

		if blockType == streaminfo.MetadataTypeStreamInfo {
			if strict && seenStreamInfo {
				return streaminfo.StreamInfo{}, nil, nil, nil, 0, errors.New("duplicate FLAC STREAMINFO block")
			}
			if length != streaminfo.Length {
				return streaminfo.StreamInfo{}, nil, nil, nil, 0, fmt.Errorf("invalid FLAC STREAMINFO length: %d", length)
			}
			block := make([]byte, length)
			if _, err := io.ReadFull(r, block); err != nil {
				return streaminfo.StreamInfo{}, nil, nil, nil, 0, fmt.Errorf("read FLAC metadata block: %w", err)
			}
			info, err := streaminfo.Parse(block)
			if err != nil {
				return streaminfo.StreamInfo{}, nil, nil, nil, 0, err
			}
			parsedInfo = info
			streamInfoBlock = block
			seenStreamInfo = true
		} else if strict && !seenStreamInfo {
			return streaminfo.StreamInfo{}, nil, nil, nil, 0, errors.New("FLAC STREAMINFO must be the first metadata block")
		} else {
			extra := make([]byte, 4+length)
			copy(extra[:4], header[:])
			if _, err := io.ReadFull(r, extra[4:]); err != nil {
				return streaminfo.StreamInfo{}, nil, nil, nil, 0, fmt.Errorf("read FLAC metadata block: %w", err)
			}
			extraBlocks = append(extraBlocks, extra)
			if blockType == seektable.MetadataType {
				if points, err := seektable.Parse(extra[4:]); err == nil {
					seekPoints = append(seekPoints, points...)
				}
			}
		}

		if isLast {
			break
		}
	}

	if !seenStreamInfo {
		return streaminfo.StreamInfo{}, nil, nil, nil, 0, errors.New("missing FLAC STREAMINFO block")
	}

	audioOffset, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return streaminfo.StreamInfo{}, nil, nil, nil, 0, err
	}
	return parsedInfo, streamInfoBlock, extraBlocks, seekPoints, audioOffset, nil
}
