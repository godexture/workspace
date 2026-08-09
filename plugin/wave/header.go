package wave

import (
	"errors"

	"github.com/godexture/godec/media/sample"
)

const (
	tagRIFF = "RIFF"
	tagRF64 = "RF64"
	tagWAVE = "WAVE"
	tagDS64 = "ds64"
	tagFMT  = "fmt "
	tagDATA = "data"

	formatPCM        = uint16(1)
	formatExtensible = uint16(0xfffe)
)

var (
	ErrMalformed     = errors.New("malformed WAVE stream")
	ErrUnsupported   = errors.New("unsupported WAVE stream")
	ErrTruncatedData = errors.New("WAVE data chunk is truncated")
	ErrPartialBlock  = errors.New("WAVE data ends inside a PCM block")
)

var extensibleBase = [12]byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}

type header struct {
	description sample.Description
	dataOffset  int64
	dataSize    uint64
	blockAlign  int
	rf64        bool
}

func (h header) valid() bool {
	return h.description.Valid() && h.dataOffset >= 0 && h.blockAlign > 0 && h.dataSize%uint64(h.blockAlign) == 0
}
