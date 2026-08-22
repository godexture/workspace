package mp4

import (
	"errors"
	"fmt"
	"math"
	"unsafe"

	mediasample "github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/resource"
)

var (
	// ErrMalformed reports an ISO BMFF structure that cannot describe a valid
	// source for this reader.
	ErrMalformed = errors.New("malformed MP4 movie")
	// ErrUnsupported reports a valid ISO BMFF feature outside the current
	// packet-reader boundary.
	ErrUnsupported = errors.New("unsupported MP4 movie")
	// ErrTruncated reports a source that ends before an inspected MP4 range.
	ErrTruncated = errors.New("truncated MP4 movie")

	errMalformedMovie   = ErrMalformed
	errUnsupportedMovie = ErrUnsupported
	errTruncatedMovie   = ErrTruncated
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
	typeSOWT = boxType{'s', 'o', 'w', 't'}
	typeTWOS = boxType{'t', 'w', 'o', 's'}
	typeEDTS = boxType{'e', 'd', 't', 's'}
	typeELST = boxType{'e', 'l', 's', 't'}
	typeTREF = boxType{'t', 'r', 'e', 'f'}
	typeSIDX = boxType{'s', 'i', 'd', 'x'}
	typeSSIX = boxType{'s', 's', 'i', 'x'}
	typeMFRA = boxType{'m', 'f', 'r', 'a'}
	typeTFRA = boxType{'t', 'f', 'r', 'a'}
	typeMETA = boxType{'m', 'e', 't', 'a'}
	typeILOC = boxType{'i', 'l', 'o', 'c'}
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
	header    movieHeader
	media     box
	tracks    []track
	// offsetIndex marks a known box that records byte offsets a rebuilt mdat
	// would invalidate.
	offsetIndex bool
	// totalSampleBytes is the checked sum of every inspected sample payload.
	// A preserving remux can reuse the mdat header only when this covers the
	// complete mdat payload.
	totalSampleBytes uint64
}

// movieHeader is the parsed mvhd: its source range, the movie timescale and the
// duration a rewritten movie patches in place.
type movieHeader struct {
	box       box
	timeScale uint32
	duration  durationField
}

// trackHeader is the parsed tkhd: the track ID and the track duration measured
// in movie timescale units.
type trackHeader struct {
	id       uint32
	duration durationField
}

// durationField locates a duration inside its header box so a rewritten movie
// can patch the value without re-reading the source, and records its width.
type durationField struct {
	offset uint64
	value  uint64
	wide   bool
}

func (d durationField) width() uint64 {
	if d.wide {
		return 8
	}
	return 4
}

// unknown reports the all-ones value ISO BMFF writes when a duration cannot be
// determined. A rewritten movie cannot derive a shorter duration from it.
func (d durationField) unknown() bool {
	if d.wide {
		return d.value == math.MaxUint64
	}
	return d.value == math.MaxUint32
}

func (d durationField) fits(value uint64) bool {
	return d.wide || value <= math.MaxUint32
}

type fileType struct {
	major boxType
	minor uint32
}

// track holds the fixed set of ranges needed to re-read its sample tables.
// Unknown boxes are deliberately not indexed: a preserving mux rescans the
// enclosing known range from the borrowed source.
type track struct {
	id          uint32
	timeScale   uint32
	duration    uint64
	sampleBytes uint64
	// movieDuration is the tkhd duration, in movie timescale units.
	movieDuration    durationField
	sampleCount      uint64
	chunkCount       uint32
	maxSampleSize    uint32
	handler          boxType
	codec            boxType
	descriptionCount uint32
	// audio is the linear PCM description of the sample entry, zero when this
	// reader cannot express the entry as decodable PCM.
	audio          mediasample.Description
	dataReferences uint32

	trak       box
	media      box
	mediaHead  box
	handlerBox box
	mediaInfo  box
	dataInfo   box
	sampleBox  box
	tables     sampleTables
	// references reports a tref child. Its target track IDs are not retained,
	// so a track subset fails closed rather than leave a reference dangling.
	references bool
	// edits reports an edts child. Its entries map media time onto the
	// presentation timeline, which a copied trak carries along unchanged but a
	// decoded track would drop, so an edited track is never described as
	// decodable PCM.
	edits bool
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
	chunkStart       bool
	sequence         uint64
}

type movieBudget struct {
	remaining uint64
}

func (m movie) valid() bool {
	if m.sourceEnd == 0 || m.fileBox.typeID != typeFTYP || m.moov.typeID != typeMOOV || m.header.box.typeID != typeMVHD || m.media.typeID != typeMDAT || len(m.tracks) == 0 {
		return false
	}
	for _, track := range m.tracks {
		if !track.valid() {
			return false
		}
	}
	return true
}

func (t track) valid() bool {
	return t.id != 0 && t.timeScale != 0 && t.descriptionCount != 0 && t.tables.timing.typeID == typeSTTS && t.tables.layout.typeID == typeSTSC && (t.tables.sizes.typeID == typeSTSZ || t.tables.sizes.typeID == typeSTZ2) && (t.tables.offsets.typeID == typeSTCO || t.tables.offsets.typeID == typeCO64)
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
