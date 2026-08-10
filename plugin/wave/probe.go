package wave

import (
	"errors"
	"io"

	"github.com/godexture/godec/access"
	mediaformat "github.com/godexture/godec/media/format"
)

func probeWAVE(ctx mediaformat.ProbeContext) (mediaformat.ProbeResult, error) {
	request, err := access.NewRangeRequest(0, 12)
	if err != nil {
		return mediaformat.ProbeResult{}, err
	}
	var signature [12]byte
	count, err := readProbePrefix(ctx, signature[:])
	if err != nil {
		return mediaformat.ProbeResult{}, err
	}
	if count != len(signature) {
		if end, known := ctx.End(); known && end <= int64(count) {
			return mediaformat.Mismatch(), nil
		}
		return mediaformat.Need(request), nil
	}
	if (string(signature[0:4]) != tagRIFF && string(signature[0:4]) != tagRF64) || string(signature[8:12]) != tagWAVE {
		return mediaformat.Mismatch(), nil
	}
	evidence, err := mediaformat.NewEvidence("RIFF/WAVE signature")
	if err != nil {
		return mediaformat.ProbeResult{}, err
	}
	return mediaformat.Match(evidence), nil
}

func readProbePrefix(ctx mediaformat.ProbeContext, destination []byte) (int, error) {
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
