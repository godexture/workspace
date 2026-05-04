package internal

import (
	"bufio"
	"errors"
	"io"

	"github.com/godexture/core/domain/manifest"
)

func Probe(r io.Reader) manifest.ProbeScore {
	reader := bufio.NewReader(r)
	header, err := reader.Peek(12)
	if err != nil {
		return manifest.ProbeMismatch
	}

	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return manifest.ProbeMismatch
	}

	chunk, err := reader.Peek(36)
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) || errors.Is(err, io.EOF) {
			return manifest.ProbeIncompleteSignature
		}
		return manifest.ProbeMismatch
	}

	if len(chunk) >= 16 && string(chunk[12:16]) == "fmt " {
		return manifest.ProbeExactSignature
	}

	return manifest.ProbeGenericContainer
}
