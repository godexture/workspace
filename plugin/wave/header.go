package wave

import (
	"errors"

	mediacodec "github.com/godexture/godec/media/codec"
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
	formatMSADPCM    = uint16(2)
	formatIMAADPCM   = uint16(0x11)
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
	geometry    blockGeometry
	metadata    metadata.Document
	ranges      sourceRanges
	// infoMemoryLimit is handed off by Inspect for a source-aware INFO
	// rewrite. It is immutable after inspection and is deliberately separate
	// from the source-range layout.
	infoMemoryLimit uint64
}

func (h header) valid() bool {
	return h.signal.Valid() && h.dataOffset >= 0 && h.blockAlign > 0 && h.dataSize%uint64(h.blockAlign) == 0 && h.codecTag.Valid()
}

// linear reports whether the stream stores its samples one scalar each, which
// is the only shape this reader can hand to a sample-level consumer.
func (h header) linear() bool { return h.description.Valid() }

// formatChunkCap bounds how much of a fmt chunk is read. A codec extension is
// the only unbounded part, and no codec this reader names needs more.
const formatChunkCap = 4 << 10

// formatChunk is everything a fmt chunk states. Every stream states a signal
// and a codec; only one whose samples are stored one scalar each also states a
// description, and only a codec whose extension WAVE does not define carries
// parameters.
type formatChunk struct {
	signal      sample.Signal
	description sample.Description
	codec       waveCodec
	blockAlign  int
	// geometry is what a block-coded stream states that a signal does not.
	geometry blockGeometry
}

// blockGeometry is what a block-coded stream states and a signal does not: how
// large a coded block is, how fast the blocks arrive, and the extension its
// codec reads. This reader reproduces all three rather than deriving them,
// because how many samples a block holds is the codec to know.
type blockGeometry struct {
	align      int
	byteRate   uint32
	parameters mediacodec.Parameters
}

func (g blockGeometry) stated() bool { return g.align != 0 }
