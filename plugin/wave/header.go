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
	tagFACT = "fact"

	// reserveOffset is where a writer places the ds64 placeholder: the first
	// chunk position, immediately after the RIFF/WAVE signature.
	reserveOffset   = 12
	ds64PayloadSize = 28

	formatPCM        = uint16(1)
	formatFloat      = uint16(3)
	formatALaw       = uint16(6)
	formatULaw       = uint16(7)
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
	// signal is what the stream is; description adds how its samples are
	// stored and is the zero value for a companded or compressed stream.
	signal      sample.Signal
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
	return h.signal.Valid() && h.dataOffset >= 0 && h.blockAlign > 0 && h.dataSize%uint64(h.blockAlign) == 0 && h.codecTag.Valid()
}

// linear reports whether the stream stores its samples one scalar each, which
// is the only shape this reader can hand to a sample-level consumer.
func (h header) linear() bool { return h.description.Valid() }
