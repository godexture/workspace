package mp4

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/godec/media/metadata"
)

type ilstType [4]byte

type ilstAtom struct {
	typeID       ilstType
	offset       int
	size         int
	payloadStart int
	payloadEnd   int
}

var (
	ilstData      = ilstType{'d', 'a', 't', 'a'}
	ilstName      = ilstType{0xa9, 'n', 'a', 'm'}
	ilstArt       = ilstType{0xa9, 'A', 'R', 'T'}
	ilstAlbum     = ilstType{0xa9, 'a', 'l', 'b'}
	ilstComposer  = ilstType{0xa9, 'w', 'r', 't'}
	ilstGenre     = ilstType{0xa9, 'g', 'e', 'n'}
	ilstDate      = ilstType{0xa9, 'd', 'a', 'y'}
	ilstComment   = ilstType{0xa9, 'c', 'm', 't'}
	ilstLyrics    = ilstType{0xa9, 'l', 'y', 'r'}
	ilstCopyright = ilstType{'c', 'p', 'r', 't'}
	ilstEncoder   = ilstType{0xa9, 't', 'o', 'o'}
	ilstTrack     = ilstType{'t', 'r', 'k', 'n'}
	ilstDisc      = ilstType{'d', 'i', 's', 'k'}
	ilstCover     = ilstType{'c', 'o', 'v', 'r'}
)

func ilstScan(payload metadata.Blob, start, end int) ([]ilstAtom, error) {
	if start < 0 || end < start || end > payload.Len() {
		return nil, fmt.Errorf("%w: invalid atom range", errIlstMalformed)
	}
	result := make([]ilstAtom, 0, 4)
	for offset := start; offset < end; {
		if end-offset < 8 {
			return nil, fmt.Errorf("%w: truncated atom header", errIlstMalformed)
		}
		var header [16]byte
		if _, err := payload.Reader().ReadAt(header[:8], int64(offset)); err != nil {
			return nil, fmt.Errorf("%w: atom header: %v", errIlstMalformed, err)
		}
		declared := uint64(binary.BigEndian.Uint32(header[:4]))
		headerSize := 8
		if declared == 0 {
			return nil, fmt.Errorf("%w: nested atom has size zero", errIlstMalformed)
		}
		if declared == 1 {
			if end-offset < 16 {
				return nil, fmt.Errorf("%w: truncated large atom header", errIlstMalformed)
			}
			if _, err := payload.Reader().ReadAt(header[8:], int64(offset+8)); err != nil {
				return nil, fmt.Errorf("%w: large atom header: %v", errIlstMalformed, err)
			}
			declared = binary.BigEndian.Uint64(header[8:])
			headerSize = 16
		}
		if declared < uint64(headerSize) || declared > uint64(end-offset) || declared > uint64(math.MaxInt) {
			return nil, fmt.Errorf("%w: atom size exceeds its parent", errIlstMalformed)
		}
		size := int(declared)
		value := ilstAtom{offset: offset, size: size, payloadStart: offset + headerSize, payloadEnd: offset + size}
		copy(value.typeID[:], header[4:8])
		result = append(result, value)
		offset += size
	}
	return result, nil
}

func ilstAtomBlob(payload metadata.Blob, atom ilstAtom, mediaType string) (metadata.Blob, bool) {
	return payload.Slice(mediaType, atom.offset, atom.offset+atom.size)
}

func ilstAtomString(value ilstType) string { return string(value[:]) }

func ilstAtomSize(payload int) (int, bool) {
	if payload < 0 || payload > math.MaxInt-8 {
		return 0, false
	}
	short := payload + 8
	if uint64(short) <= math.MaxUint32 {
		return short, true
	}
	if payload > math.MaxInt-16 {
		return 0, false
	}
	return payload + 16, true
}

func ilstAppendAtom(destination []byte, typeID ilstType, payloadSize int, appendPayload func([]byte) []byte) []byte {
	size, ok := ilstAtomSize(payloadSize)
	if !ok {
		panic("invalid ilst atom size")
	}
	if uint64(size) <= math.MaxUint32 {
		destination = binary.BigEndian.AppendUint32(destination, uint32(size))
		destination = append(destination, typeID[:]...)
	} else {
		destination = binary.BigEndian.AppendUint32(destination, 1)
		destination = append(destination, typeID[:]...)
		destination = binary.BigEndian.AppendUint64(destination, uint64(size))
	}
	return appendPayload(destination)
}
