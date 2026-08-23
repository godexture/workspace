package wave

import (
	"errors"

	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/sample"
)

const (
	tagRIFF = "RIFF"
	tagRF64 = "RF64"
	tagWAVE = "WAVE"
	tagDS64 = "ds64"
	tagJUNK = "JUNK"
	tagFMT  = "fmt "
	tagDATA = "data"

	// reserveOffset is where a writer places the ds64 placeholder: the first
	// chunk position, immediately after the RIFF/WAVE signature.
	reserveOffset   = 12
	ds64PayloadSize = 28

	formatPCM        = uint16(1)
	formatFloat      = uint16(3)
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
	rootEnd     uint64
	sourceSize  uint64
	blockAlign  int
	rf64        bool
	codecTag    format.Tag
	metadata    metadata.Document
	ranges      sourceRanges
}

func (h header) valid() bool {
	return h.description.Valid() && h.dataOffset >= 0 && h.blockAlign > 0 && h.dataSize%uint64(h.blockAlign) == 0 && h.codecTag.Valid()
}
