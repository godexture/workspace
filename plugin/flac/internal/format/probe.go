package internal

import (
	"bufio"
	"errors"
	"io"

	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/plugin/flac/internal/streaminfo"
)

func Probe(r io.Reader) manifest.ProbeScore {
	reader := bufio.NewReader(r)
	header, err := reader.Peek(len(streaminfo.Marker))
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) || errors.Is(err, io.EOF) {
			if len(header) > 0 && flacMarkerHasPrefix(header) {
				return manifest.ProbeIncompleteSignature
			}
		}
		return manifest.ProbeMismatch
	}

	if string(header) != streaminfo.Marker {
		return manifest.ProbeMismatch
	}

	return manifest.ProbeExactSignature
}

func flacMarkerHasPrefix(data []byte) bool {
	if len(data) > len(streaminfo.Marker) {
		return false
	}
	for i := range data {
		if data[i] != streaminfo.Marker[i] {
			return false
		}
	}
	return true
}
