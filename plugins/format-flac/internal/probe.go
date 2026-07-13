package internal

import (
	"bufio"
	"errors"
	"io"

	"github.com/godexture/core/domain/manifest"
)

func Probe(r io.Reader) manifest.ProbeScore {
	reader := bufio.NewReader(r)
	header, err := reader.Peek(len(flacMarker))
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) || errors.Is(err, io.EOF) {
			if len(header) > 0 && flacMarkerHasPrefix(header) {
				return manifest.ProbeIncompleteSignature
			}
		}
		return manifest.ProbeMismatch
	}

	if string(header) != flacMarker {
		return manifest.ProbeMismatch
	}

	return manifest.ProbeExactSignature
}

func flacMarkerHasPrefix(data []byte) bool {
	if len(data) > len(flacMarker) {
		return false
	}
	for i := range data {
		if data[i] != flacMarker[i] {
			return false
		}
	}
	return true
}
