package mp4

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/godexture/godec/access"
	mediaformat "github.com/godexture/godec/media/format"
)

const mp4ProbeLimit = 256

func probeMP4(ctx mediaformat.ProbeContext) (mediaformat.ProbeResult, error) {
	initial, err := access.NewRangeRequest(0, 8)
	if err != nil {
		return mediaformat.ProbeResult{}, err
	}
	var header [8]byte
	count, err := readProbePrefix(ctx, header[:])
	if err != nil {
		return mediaformat.ProbeResult{}, err
	}
	if count != len(header) {
		if end, known := ctx.End(); known && end <= int64(count) {
			return mediaformat.Mismatch(), nil
		}
		return mediaformat.Need(initial), nil
	}
	if boxType(header[4:8]) != typeFTYP {
		return mediaformat.Mismatch(), nil
	}

	request, err := access.NewRangeRequest(0, 16)
	if err != nil {
		return mediaformat.ProbeResult{}, err
	}
	var prefix [16]byte
	count, err = readProbePrefix(ctx, prefix[:])
	if err != nil {
		return mediaformat.ProbeResult{}, err
	}
	if count != len(prefix) {
		if end, known := ctx.End(); known && end <= int64(count) {
			return mediaformat.Mismatch(), nil
		}
		return mediaformat.Need(request), nil
	}
	declared := uint64(binary.BigEndian.Uint32(prefix[:4]))
	if declared == 0 || declared < uint64(len(prefix)) || declared == 1 || (declared-uint64(len(prefix)))%4 != 0 {
		return mediaformat.Mismatch(), nil
	}
	major := boxType(prefix[8:12])
	if knownMP4Brand(major) {
		return mp4ProbeMatch()
	}
	if declared > mp4ProbeLimit {
		return mediaformat.Mismatch(), nil
	}
	request, err = access.NewRangeRequest(0, int64(declared))
	if err != nil {
		return mediaformat.ProbeResult{}, err
	}
	data := make([]byte, int(declared))
	count, err = readProbePrefix(ctx, data)
	if err != nil {
		return mediaformat.ProbeResult{}, err
	}
	if count != len(data) {
		if end, known := ctx.End(); known && end <= int64(count) {
			return mediaformat.Mismatch(), nil
		}
		return mediaformat.Need(request), nil
	}
	for offset := 16; offset < len(data); offset += 4 {
		if knownMP4Brand(boxType(data[offset : offset+4])) {
			return mp4ProbeMatch()
		}
	}
	return mediaformat.Mismatch(), nil
}

func mp4ProbeMatch() (mediaformat.ProbeResult, error) {
	evidence, err := mediaformat.NewEvidence("ISO BMFF ftyp brand")
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
