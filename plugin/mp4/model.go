package mp4

import (
	"errors"
	"fmt"
	"math"
	"unsafe"

	"github.com/godexture/godec/resource"
)

var (
	errMalformedMovie   = errors.New("malformed MP4 movie")
	errUnsupportedMovie = errors.New("unsupported MP4 movie")
	errTruncatedMovie   = errors.New("truncated MP4 movie")
)

var (
	typeFTYP = boxType{'f', 't', 'y', 'p'}
	typeMOOV = boxType{'m', 'o', 'o', 'v'}
	typeMDAT = boxType{'m', 'd', 'a', 't'}
	typeMOOF = boxType{'m', 'o', 'o', 'f'}
	typeMVEX = boxType{'m', 'v', 'e', 'x'}
	typeMVHD = boxType{'m', 'v', 'h', 'd'}
	typeTRAK = boxType{'t', 'r', 'a', 'k'}
	typeTKHD = boxType{'t', 'k', 'h', 'd'}
	typeMDIA = boxType{'m', 'd', 'i', 'a'}
	typeMDHD = boxType{'m', 'd', 'h', 'd'}
	typeHDLR = boxType{'h', 'd', 'l', 'r'}
	typeMINF = boxType{'m', 'i', 'n', 'f'}
	typeDINF = boxType{'d', 'i', 'n', 'f'}
	typeDREF = boxType{'d', 'r', 'e', 'f'}
	typeSTBL = boxType{'s', 't', 'b', 'l'}
	typeSTSD = boxType{'s', 't', 's', 'd'}
	typeSTTS = boxType{'s', 't', 't', 's'}
	typeCTTS = boxType{'c', 't', 't', 's'}
	typeSTSC = boxType{'s', 't', 's', 'c'}
	typeSTSZ = boxType{'s', 't', 's', 'z'}
	typeSTZ2 = boxType{'s', 't', 'z', '2'}
	typeSTCO = boxType{'s', 't', 'c', 'o'}
	typeCO64 = boxType{'c', 'o', '6', '4'}
	typeSTSS = boxType{'s', 't', 's', 's'}
	typeEDTS = boxType{'e', 'd', 't', 's'}
	typeELST = boxType{'e', 'l', 's', 't'}
	typeENCV = boxType{'e', 'n', 'c', 'v'}
	typeENCA = boxType{'e', 'n', 'c', 'a'}
	typeURL  = boxType{'u', 'r', 'l', ' '}
	typeURN  = boxType{'u', 'r', 'n', ' '}
	typeVMHD = boxType{'v', 'm', 'h', 'd'}
	typeSMHD = boxType{'s', 'm', 'h', 'd'}
	typeHMHD = boxType{'h', 'm', 'h', 'd'}
	typeNMHD = boxType{'n', 'm', 'h', 'd'}
	typeGMHD = boxType{'g', 'm', 'h', 'd'}
)

// movie is a frozen inspection result. It contains only source ranges and
// bounded per-track summaries; the source itself is lent to a cursor at Open.
type movie struct {
	sourceEnd uint64
	fileType  fileType
	fileBox   box
	moov      box
	movieHead box
	media     box
	tracks    []track
}

type fileType struct {
	major boxType
	minor uint32
}

// track holds the fixed set of ranges needed to re-read its sample tables.
// Unknown boxes are deliberately not indexed: a preserving mux rescans the
// enclosing known range from the borrowed source.
type track struct {
	id               uint32
	timeScale        uint32
	duration         uint64
	sampleCount      uint64
	maxSampleSize    uint32
	handler          boxType
	codec            boxType
	descriptionCount uint32
	dataReferences   uint32

	trak       box
	trackHead  box
	media      box
	mediaHead  box
	handlerBox box
	mediaInfo  box
	dataInfo   box
	sampleBox  box
	tables     sampleTables
}

type sampleTables struct {
	description box
	timing      box
	composition box
	layout      box
	sizes       box
	offsets     box
	sync        box

	hasComposition bool
	hasSync        bool
	compactSizes   bool
	largeOffsets   bool
	fixedSize      uint32
}

// sample is reconstructed by sampleCursor and is never retained by movie.
type sample struct {
	offset           uint64
	size             uint32
	duration         uint32
	dts              uint64
	pts              int64
	descriptionIndex uint32
	sync             bool
	sequence         uint64
}

type movieBudget struct {
	remaining uint64
}

const trackBudgetBytes = uint64(unsafe.Sizeof(track{}))

func newMovieBudget(memoryLimit resource.Bytes) movieBudget {
	return movieBudget{remaining: uint64(memoryLimit)}
}

func (b *movieBudget) reserve(size uint64, what string) error {
	if size > b.remaining || size > uint64(math.MaxInt) {
		return fmt.Errorf("%w: %s needs %d bytes with %d remaining", errUnsupportedMovie, what, size, b.remaining)
	}
	b.remaining -= size
	return nil
}

func (b *movieBudget) reserveTracks(count uint64) error {
	if count != 0 && count > math.MaxUint64/trackBudgetBytes {
		return fmt.Errorf("%w: track count overflows", errMalformedMovie)
	}
	return b.reserve(count*trackBudgetBytes, "track model")
}

func boxTypeOf(value string) boxType {
	var result boxType
	copy(result[:], value)
	return result
}

func knownMP4Brand(value boxType) bool {
	switch value {
	case boxType{'i', 's', 'o', 'm'},
		boxType{'i', 's', 'o', '2'},
		boxType{'i', 's', 'o', '3'},
		boxType{'i', 's', 'o', '4'},
		boxType{'i', 's', 'o', '5'},
		boxType{'i', 's', 'o', '6'},
		boxType{'i', 's', 'o', '7'},
		boxType{'i', 's', 'o', '8'},
		boxType{'i', 's', 'o', '9'},
		boxType{'m', 'p', '4', '1'},
		boxType{'m', 'p', '4', '2'},
		boxType{'a', 'v', 'c', '1'},
		boxType{'M', '4', 'A', ' '},
		boxType{'M', '4', 'V', ' '}:
		return true
	default:
		return false
	}
}
