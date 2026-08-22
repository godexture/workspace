package mp4

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
	mediasample "github.com/godexture/godec/media/sample"
)

const (
	// baseEntryBytes is the SampleEntry header every stsd entry starts with.
	baseEntryBytes = 16
	// audioEntryBytes additionally covers the channel count, sample size and
	// sample rate an AudioSampleEntry places at fixed offsets.
	audioEntryBytes = 36
)

// sampleEntry summarizes the stsd table: how many entries it holds and, for the
// first one, its four-character code and the audio description it carries when
// this reader can express it as linear PCM.
type sampleEntry struct {
	count  uint32
	typeID boxType
	audio  mediasample.Description
}

func scanSampleDescriptions(ctx context.Context, reader access.Random, value box, dataReferences uint32) (sampleEntry, error) {
	if value.payloadSize < 8 {
		return sampleEntry{}, fmt.Errorf("%w: stsd full-box header", errUnsupportedMovie)
	}
	var prefix [8]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:], "stsd"); err != nil {
		return sampleEntry{}, err
	}
	version, flags, err := fullBox(prefix[:4], "stsd")
	if err != nil {
		return sampleEntry{}, err
	}
	if version != 0 || flags != 0 {
		return sampleEntry{}, fmt.Errorf("%w: stsd full-box header", errUnsupportedMovie)
	}
	result := sampleEntry{count: binary.BigEndian.Uint32(prefix[4:])}
	if result.count == 0 {
		return sampleEntry{}, fmt.Errorf("%w: stsd has no entries", errMalformedMovie)
	}
	offset := uint64(8)
	payloadEnd, ok := payloadEnd(value)
	if !ok {
		return sampleEntry{}, fmt.Errorf("%w: stsd payload range", errMalformedMovie)
	}
	entries := newTableRangeReader(reader, payloadEnd)
	for index := uint32(0); index < result.count; index++ {
		var entry [audioEntryBytes]byte
		if offset > value.payloadSize || value.payloadSize-offset < baseEntryBytes {
			return sampleEntry{}, fmt.Errorf("%w: stsd entry %d is truncated", errMalformedMovie, index+1)
		}
		entryOffset, ok := checkedBoxAdd(value.payloadOffset, offset)
		if !ok {
			return sampleEntry{}, fmt.Errorf("%w: stsd entry %d range", errMalformedMovie, index+1)
		}
		if err := entries.readAt(ctx, entryOffset, entry[:baseEntryBytes], "stsd"); err != nil {
			return sampleEntry{}, err
		}
		size := uint64(binary.BigEndian.Uint32(entry[:4]))
		if size < baseEntryBytes || size > value.payloadSize-offset {
			return sampleEntry{}, fmt.Errorf("%w: stsd entry %d size", errMalformedMovie, index+1)
		}
		typeID := boxType(entry[4:8])
		if typeID == typeENCV || typeID == typeENCA {
			return sampleEntry{}, fmt.Errorf("%w: encrypted sample entry %q", errUnsupportedMovie, typeID)
		}
		dataReference := binary.BigEndian.Uint16(entry[14:16])
		if dataReference == 0 || uint32(dataReference) > dataReferences {
			return sampleEntry{}, fmt.Errorf("%w: stsd entry %d data reference", errMalformedMovie, index+1)
		}
		if index == 0 {
			result.typeID = typeID
			// An entry too short to carry the audio fields stays opaque, like
			// any other shape parseAudioEntry cannot express: the track is
			// copied rather than decoded, and the movie remains usable.
			if endian, linear := linearPCMEntry(typeID); linear && size >= audioEntryBytes {
				if err := entries.readAt(ctx, entryOffset, entry[:], "stsd"); err != nil {
					return sampleEntry{}, err
				}
				result.audio = parseAudioEntry(entry[:], endian)
			}
		}
		next, ok := checkedBoxAdd(offset, size)
		if !ok {
			return sampleEntry{}, fmt.Errorf("%w: stsd entry %d range", errMalformedMovie, index+1)
		}
		offset = next
	}
	if offset != value.payloadSize {
		return sampleEntry{}, fmt.Errorf("%w: stsd has trailing bytes", errMalformedMovie)
	}
	return result, nil
}

// linearPCMEntry reports the byte order of the ISO BMFF sample entries this
// reader describes as signed 16-bit linear PCM. Wider and floating-point entries
// stay opaque and are copied rather than decoded.
func linearPCMEntry(typeID boxType) (mediasample.Endian, bool) {
	switch typeID {
	case typeSOWT:
		return mediasample.LittleEndian, true
	case typeTWOS:
		return mediasample.BigEndian, true
	default:
		return mediasample.NoEndian, false
	}
}

// parseAudioEntry reads the channel count, sample size and 16.16 sample rate an
// AudioSampleEntry places at the same offsets in its version 0 and version 1
// layouts. A shape this decoder cannot express yields the zero description, so
// the track is copied instead of decoded.
func parseAudioEntry(data []byte, endian mediasample.Endian) mediasample.Description {
	if len(data) < audioEntryBytes {
		return mediasample.Description{}
	}
	var layout mediasample.Layout
	switch binary.BigEndian.Uint16(data[24:26]) {
	case 1:
		layout = mediasample.Mono
	case 2:
		layout = mediasample.Stereo
	default:
		return mediasample.Description{}
	}
	if binary.BigEndian.Uint16(data[26:28]) != 16 {
		return mediasample.Description{}
	}
	rate := binary.BigEndian.Uint32(data[32:36])
	if rate>>16 == 0 || rate&0xffff != 0 {
		return mediasample.Description{}
	}
	return mediasample.Description{
		Format:    mediasample.S16Interleaved,
		ValidBits: 16,
		Rate:      int(rate >> 16),
		Layout:    layout,
		Endian:    endian,
	}
}

func scanTimingTable(ctx context.Context, reader access.Random, value box) (uint64, uint64, error) {
	entries, err := newTableReader(ctx, reader, value, "stts", 8, 0, false)
	if err != nil {
		return 0, 0, err
	}
	var samples, duration uint64
	count := entries.remaining
	for index := uint32(0); index < count; index++ {
		entry, err := entries.next(ctx)
		if err != nil {
			return 0, 0, err
		}
		count := binary.BigEndian.Uint32(entry[:4])
		if count == 0 {
			return 0, 0, fmt.Errorf("%w: stts entry %d has zero samples", errMalformedMovie, index+1)
		}
		next, ok := checkedBoxAdd(samples, uint64(count))
		if !ok {
			return 0, 0, fmt.Errorf("%w: stts sample count overflows", errMalformedMovie)
		}
		samples = next
		span := uint64(count) * uint64(binary.BigEndian.Uint32(entry[4:8]))
		if count != 0 && span/uint64(count) != uint64(binary.BigEndian.Uint32(entry[4:8])) {
			return 0, 0, fmt.Errorf("%w: stts duration overflows", errMalformedMovie)
		}
		next, ok = checkedBoxAdd(duration, span)
		if !ok {
			return 0, 0, fmt.Errorf("%w: DTS overflows", errMalformedMovie)
		}
		duration = next
	}
	return samples, duration, nil
}

func scanCompositionTable(ctx context.Context, reader access.Random, value box) (uint64, error) {
	entries, err := newTableReader(ctx, reader, value, "ctts", 8, 0, true)
	if err != nil {
		return 0, err
	}
	var samples uint64
	count := entries.remaining
	for index := uint32(0); index < count; index++ {
		entry, err := entries.next(ctx)
		if err != nil {
			return 0, err
		}
		count := binary.BigEndian.Uint32(entry[:4])
		if count == 0 {
			return 0, fmt.Errorf("%w: ctts entry %d has zero samples", errMalformedMovie, index+1)
		}
		next, ok := checkedBoxAdd(samples, uint64(count))
		if !ok {
			return 0, fmt.Errorf("%w: ctts sample count overflows", errMalformedMovie)
		}
		samples = next
	}
	return samples, nil
}

func scanSampleSizes(ctx context.Context, reader access.Random, value box, compact bool) (uint64, uint32, uint32, error) {
	var prefix [12]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:], "sample sizes"); err != nil {
		return 0, 0, 0, err
	}
	version, flags, err := fullBox(prefix[:4], "sample sizes")
	if err != nil {
		return 0, 0, 0, err
	}
	if version != 0 || flags != 0 || value.payloadSize < uint64(len(prefix)) {
		return 0, 0, 0, fmt.Errorf("%w: %s full-box header", errUnsupportedMovie, value.typeID)
	}
	count := binary.BigEndian.Uint32(prefix[8:])
	if !compact {
		fixed := binary.BigEndian.Uint32(prefix[4:8])
		entries := uint64(0)
		if fixed == 0 {
			entries = uint64(count) * 4
		}
		if count != 0 && fixed == 0 && entries/4 != uint64(count) || entries > math.MaxUint64-12 || value.payloadSize != 12+entries {
			return 0, 0, 0, fmt.Errorf("%w: stsz entries", errMalformedMovie)
		}
		if fixed != 0 {
			return uint64(count), fixed, fixed, nil
		}
		entriesReader := rawTableReader{reader: reader, offset: value.payloadOffset + 12, remaining: count, entrySize: 4, what: "stsz"}
		var maxSize uint32
		for entriesReader.remaining > 0 {
			entry, err := entriesReader.next(ctx)
			if err != nil {
				return 0, 0, 0, err
			}
			if size := binary.BigEndian.Uint32(entry[:4]); size > maxSize {
				maxSize = size
			}
		}
		return uint64(count), maxSize, 0, nil
	}
	bits := prefix[7]
	if bits != 4 && bits != 8 && bits != 16 {
		return 0, 0, 0, fmt.Errorf("%w: stz2 field size %d", errUnsupportedMovie, bits)
	}
	bytes, ok := compactSizeBytes(uint64(count), bits)
	if !ok || value.payloadSize != 12+bytes {
		return 0, 0, 0, fmt.Errorf("%w: stz2 entries", errMalformedMovie)
	}
	maxSize, err := scanCompactMax(ctx, reader, value.payloadOffset+12, bytes, bits)
	if err != nil {
		return 0, 0, 0, err
	}
	return uint64(count), maxSize, 0, nil
}

func scanCompactMax(ctx context.Context, reader access.Random, offset, bytes uint64, bits byte) (uint32, error) {
	var maxSize uint32
	switch bits {
	case 4, 8:
		entries := rawTableReader{reader: reader, offset: offset, remaining: uint32(bytes), entrySize: 1, what: "stz2"}
		for entries.remaining > 0 {
			entry, err := entries.next(ctx)
			if err != nil {
				return 0, err
			}
			if bits == 4 {
				if size := uint32(entry[0] >> 4); size > maxSize {
					maxSize = size
				}
				if size := uint32(entry[0] & 0x0f); size > maxSize {
					maxSize = size
				}
			} else if size := uint32(entry[0]); size > maxSize {
				maxSize = size
			}
		}
	case 16:
		entries := rawTableReader{reader: reader, offset: offset, remaining: uint32(bytes / 2), entrySize: 2, what: "stz2"}
		for entries.remaining > 0 {
			entry, err := entries.next(ctx)
			if err != nil {
				return 0, err
			}
			if size := uint32(binary.BigEndian.Uint16(entry[:])); size > maxSize {
				maxSize = size
			}
		}
	}
	return maxSize, nil
}

func compactSizeBytes(count uint64, bits byte) (uint64, bool) {
	bitsTotal := count * uint64(bits)
	if count != 0 && bitsTotal/uint64(bits) != count {
		return 0, false
	}
	return (bitsTotal + 7) / 8, bitsTotal <= math.MaxUint64-7
}
