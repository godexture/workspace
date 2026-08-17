package mp4

import (
	"errors"
	"fmt"
	"math"

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

type movie struct {
	fileType fileType
	media    []mediaRange
	tracks   []track
	top      []anchor
	moov     []anchor
}

type fileType struct {
	major      boxType
	minor      uint32
	compatible []boxType
}

type mediaRange struct {
	start uint64
	end   uint64
}

// anchor is a direct child in one scope. A nil raw value means the box is
// regenerated from the movie model; otherwise raw is reinserted unchanged.
type anchor struct {
	typeID boxType
	index  int
	raw    []byte
}

type track struct {
	id            uint32
	timeScale     uint32
	handler       boxType
	trackHeader   []byte
	mediaHeader   []byte
	handlerHeader []byte
	dataInfo      []byte
	mediaInfo     []byte
	descriptions  []sampleDescription
	samples       []sample
	anchors       []anchor

	timing        []timingRun
	composition   []compositionRun
	chunkLayout   []chunkRun
	chunkOffsets  []uint64
	sampleSizes   []uint32
	syncSamples   []uint32
	hasSyncSample bool
}

type sampleDescription struct {
	typeID             boxType
	dataReferenceIndex uint16
	raw                []byte
}

type sample struct {
	offset           uint64
	size             uint32
	duration         uint32
	dts              uint64
	pts              int64
	descriptionIndex uint32
	sync             bool
}

type timingRun struct {
	count    uint32
	duration uint32
}

type compositionRun struct {
	count  uint32
	offset int64
}

type chunkRun struct {
	firstChunk       uint32
	samplesPerChunk  uint32
	descriptionIndex uint32
}

type movieBudget struct {
	readLimit uint64
	remaining uint64
}

const (
	anchorBudgetBytes      = uint64(64)
	trackBudgetBytes       = uint64(512)
	descriptionBudgetBytes = uint64(64)
	sampleBudgetBytes      = uint64(40)
)

func newMovieBudget(readLimit, memoryLimit resource.Bytes) movieBudget {
	return movieBudget{readLimit: uint64(readLimit), remaining: uint64(memoryLimit)}
}

func (b *movieBudget) reserve(size uint64, what string) error {
	if size > b.remaining || size > uint64(math.MaxInt) {
		return fmt.Errorf("%w: %s needs %d bytes with %d remaining", errUnsupportedMovie, what, size, b.remaining)
	}
	b.remaining -= size
	return nil
}

func (b *movieBudget) release(size uint64) {
	if math.MaxUint64-b.remaining < size {
		b.remaining = math.MaxUint64
		return
	}
	b.remaining += size
}

func (b *movieBudget) reserveRecords(count, size uint64, what string) error {
	if count != 0 && size > math.MaxUint64/count {
		return fmt.Errorf("%w: %s count overflows", errMalformedMovie, what)
	}
	return b.reserve(count*size, what)
}

func (b *movieBudget) releaseRecords(count, size uint64) {
	if count == 0 || size == 0 || size > math.MaxUint64/count {
		return
	}
	b.release(count * size)
}

func (b movieBudget) checkRead(size uint64, what string) error {
	if size > b.readLimit || size > uint64(math.MaxInt) {
		return fmt.Errorf("%w: %s needs a %d-byte read allocation", errUnsupportedMovie, what, size)
	}
	return nil
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
