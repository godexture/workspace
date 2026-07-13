package internal

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/format-flac/streaminfo"
)

func parseNativeFLACHeader(r io.ReadSeeker) (streaminfo.StreamInfo, []byte, int64, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return streaminfo.StreamInfo{}, nil, 0, err
	}

	var marker [4]byte
	if _, err := io.ReadFull(r, marker[:]); err != nil {
		return streaminfo.StreamInfo{}, nil, 0, fmt.Errorf("read FLAC marker: %w", err)
	}
	if string(marker[:]) != streaminfo.Marker {
		return streaminfo.StreamInfo{}, nil, 0, errors.New("not a native FLAC stream")
	}

	seenStreamInfo := false
	var parsedInfo streaminfo.StreamInfo
	var streamInfoBlock []byte
	for {
		var header [4]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return streaminfo.StreamInfo{}, nil, 0, fmt.Errorf("read FLAC metadata header: %w", err)
		}

		isLast, blockType, length := streaminfo.ParseBlockHeader(header)
		if length < 0 {
			return streaminfo.StreamInfo{}, nil, 0, errors.New("invalid FLAC metadata length")
		}

		block := make([]byte, length)
		if _, err := io.ReadFull(r, block); err != nil {
			return streaminfo.StreamInfo{}, nil, 0, fmt.Errorf("read FLAC metadata block: %w", err)
		}

		if blockType == streaminfo.MetadataTypeStreamInfo {
			if seenStreamInfo {
				return streaminfo.StreamInfo{}, nil, 0, errors.New("duplicate FLAC STREAMINFO block")
			}
			if length != streaminfo.Length {
				return streaminfo.StreamInfo{}, nil, 0, fmt.Errorf("invalid FLAC STREAMINFO length: %d", length)
			}
			info, err := streaminfo.Parse(block)
			if err != nil {
				return streaminfo.StreamInfo{}, nil, 0, err
			}
			parsedInfo = info
			streamInfoBlock = append([]byte(nil), block...)
			seenStreamInfo = true
		} else if !seenStreamInfo {
			return streaminfo.StreamInfo{}, nil, 0, errors.New("FLAC STREAMINFO must be the first metadata block")
		}

		if isLast {
			break
		}
	}

	if !seenStreamInfo {
		return streaminfo.StreamInfo{}, nil, 0, errors.New("missing FLAC STREAMINFO block")
	}

	audioOffset, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return streaminfo.StreamInfo{}, nil, 0, err
	}
	return parsedInfo, streamInfoBlock, audioOffset, nil
}
