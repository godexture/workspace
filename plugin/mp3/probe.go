package mp3

import (
	"errors"
	"io"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin/mp3/internal/frame"
)

func probeMP3(ctx format.ProbeContext) (format.ProbeResult, error) {
	request, err := access.NewRangeRequest(0, 4)
	if err != nil {
		return format.ProbeResult{}, err
	}
	var prefix [10]byte
	count, readErr := readProbePrefix(ctx, prefix[:])
	if readErr != nil {
		return format.ProbeResult{}, readErr
	}
	if count < 3 {
		if end, known := ctx.End(); known && end <= int64(count) {
			return format.Mismatch(), nil
		}
		return format.Need(request), nil
	}
	if string(prefix[:3]) == "ID3" {
		if count < len(prefix) {
			if end, known := ctx.End(); known && end <= int64(count) {
				evidence, evidenceErr := format.NewEvidence("ID3v2 signature")
				if evidenceErr != nil {
					return format.ProbeResult{}, evidenceErr
				}
				return format.Malformed("truncated ID3v2 header", evidence), nil
			}
			request, err = access.NewRangeRequest(0, 10)
			if err != nil {
				return format.ProbeResult{}, err
			}
			return format.Need(request), nil
		}
		if _, _, err := parseV2Header(prefix[:]); err != nil {
			evidence, evidenceErr := format.NewEvidence("ID3v2 signature")
			if evidenceErr != nil {
				return format.ProbeResult{}, evidenceErr
			}
			return format.Malformed("invalid ID3v2 header", evidence), nil
		}
		evidence, err := format.NewEvidence("ID3v2 signature")
		if err != nil {
			return format.ProbeResult{}, err
		}
		return format.Match(evidence), nil
	}
	if count < 4 {
		if end, known := ctx.End(); known && end <= int64(count) {
			return format.Mismatch(), nil
		}
		return format.Need(request), nil
	}
	if _, err := frame.Parse(prefix[:4]); err != nil {
		return format.Mismatch(), nil
	}
	evidence, err := format.NewEvidence("MPEG audio frame header")
	if err != nil {
		return format.ProbeResult{}, err
	}
	return format.Match(evidence), nil
}

func readProbePrefix(ctx format.ProbeContext, destination []byte) (int, error) {
	read := 0
	for read < len(destination) {
		found := false
		for _, view := range ctx.Views() {
			if int64(read) < view.Base() || int64(read) >= view.Base()+view.Size() {
				continue
			}
			count, err := view.ReadAt(ctx.Context(), destination[read:], int64(read))
			read += count
			found = count != 0
			if err != nil && !errors.Is(err, io.EOF) {
				return read, err
			}
			break
		}
		if !found {
			break
		}
	}
	return read, nil
}
