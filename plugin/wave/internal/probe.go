package internal

import (
	"bufio"
	"errors"
	"io"

	"github.com/godexture/godec/core/domain/manifest"
)

func Probe(r io.Reader) manifest.ProbeScore {
	reader := bufio.NewReader(r)
	header, err := reader.Peek(12)
	if err != nil {
		return manifest.ProbeMismatch
	}

	isRF64 := string(header[0:4]) == wavTagRF64
	if string(header[0:4]) != wavTagRIFF && !isRF64 {
		return manifest.ProbeMismatch
	}
	if string(header[8:12]) != wavTagWAVE {
		return manifest.ProbeMismatch
	}

	chunk, err := reader.Peek(36)
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) || errors.Is(err, io.EOF) {
			return manifest.ProbeIncompleteSignature
		}
		return manifest.ProbeMismatch
	}

	if len(chunk) >= 16 {
		sig := string(chunk[12:16])
		if sig == wavTagFmt || (isRF64 && sig == wavTagDS64) {
			return manifest.ProbeExactSignature
		}
	}

	return manifest.ProbeGenericContainer
}
