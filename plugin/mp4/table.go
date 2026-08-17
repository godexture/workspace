package mp4

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
)

func scanSampleDescriptions(ctx context.Context, reader access.Random, value box, dataReferences uint32) (uint32, boxType, error) {
	if value.payloadSize < 8 {
		return 0, boxType{}, fmt.Errorf("%w: stsd full-box header", errUnsupportedMovie)
	}
	var prefix [8]byte
	if err := readBoxPrefix(ctx, reader, value, prefix[:], "stsd"); err != nil {
		return 0, boxType{}, err
	}
	version, flags, err := fullBox(prefix[:4], "stsd")
	if err != nil {
		return 0, boxType{}, err
	}
	if version != 0 || flags != 0 {
		return 0, boxType{}, fmt.Errorf("%w: stsd full-box header", errUnsupportedMovie)
	}
	count := binary.BigEndian.Uint32(prefix[4:])
	if count == 0 {
		return 0, boxType{}, fmt.Errorf("%w: stsd has no entries", errMalformedMovie)
	}
	offset := uint64(8)
	payloadEnd, ok := payloadEnd(value)
	if !ok {
		return 0, boxType{}, fmt.Errorf("%w: stsd payload range", errMalformedMovie)
	}
	entries := newTableRangeReader(reader, value.payloadOffset+offset, payloadEnd)
	var codec boxType
	for index := uint32(0); index < count; index++ {
		var entry [16]byte
		if offset > value.payloadSize || value.payloadSize-offset < uint64(len(entry)) {
			return 0, boxType{}, fmt.Errorf("%w: stsd entry %d is truncated", errMalformedMovie, index+1)
		}
		entryOffset, ok := checkedBoxAdd(value.payloadOffset, offset)
		if !ok {
			return 0, boxType{}, fmt.Errorf("%w: stsd entry %d range", errMalformedMovie, index+1)
		}
		if err := entries.readAt(ctx, entryOffset, entry[:], "stsd"); err != nil {
			return 0, boxType{}, err
		}
		size := uint64(binary.BigEndian.Uint32(entry[:4]))
		if size < uint64(len(entry)) || size > value.payloadSize-offset {
			return 0, boxType{}, fmt.Errorf("%w: stsd entry %d size", errMalformedMovie, index+1)
		}
		typeID := boxType(entry[4:8])
		if typeID == typeENCV || typeID == typeENCA {
			return 0, boxType{}, fmt.Errorf("%w: encrypted sample entry %q", errUnsupportedMovie, typeID)
		}
		dataReference := binary.BigEndian.Uint16(entry[14:16])
		if dataReference == 0 || uint32(dataReference) > dataReferences {
			return 0, boxType{}, fmt.Errorf("%w: stsd entry %d data reference", errMalformedMovie, index+1)
		}
		if index == 0 {
			codec = typeID
		}
		next, ok := checkedBoxAdd(offset, size)
		if !ok {
			return 0, boxType{}, fmt.Errorf("%w: stsd entry %d range", errMalformedMovie, index+1)
		}
		offset = next
	}
	if offset != value.payloadSize {
		return 0, boxType{}, fmt.Errorf("%w: stsd has trailing bytes", errMalformedMovie)
	}
	return count, codec, nil
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
