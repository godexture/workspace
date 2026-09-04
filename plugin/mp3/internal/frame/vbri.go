package frame

import "encoding/binary"

const (
	vbriOffset      = 36
	vbriHeaderBytes = 26
)

// ParseVBRI parses the fixed-position VBRI index from a frame prefix. VBRI is
// stored 32 bytes after the MPEG frame header, so its marker starts at byte 36.
func ParseVBRI(header Header, frame []byte) (VBRI, bool, error) {
	if !header.Valid() {
		return VBRI{}, false, ErrInvalidHeader
	}
	if header.Layer() != LayerIII {
		return VBRI{}, false, nil
	}
	if len(frame) < vbriOffset+4 {
		return VBRI{}, false, nil
	}
	if string(frame[vbriOffset:vbriOffset+4]) != "VBRI" {
		return VBRI{}, false, nil
	}
	if len(frame) < vbriOffset+vbriHeaderBytes {
		return VBRI{}, true, ErrIndexTooShort
	}

	offset := vbriOffset + 4
	version, ok := readUint16(frame, &offset)
	if !ok {
		return VBRI{}, true, ErrIndexTooShort
	}
	delay, ok := readUint16(frame, &offset)
	if !ok {
		return VBRI{}, true, ErrIndexTooShort
	}
	quality, ok := readUint16(frame, &offset)
	if !ok {
		return VBRI{}, true, ErrIndexTooShort
	}
	bytes, ok := readUint32(frame, &offset)
	if !ok {
		return VBRI{}, true, ErrIndexTooShort
	}
	frames, ok := readUint32(frame, &offset)
	if !ok {
		return VBRI{}, true, ErrIndexTooShort
	}
	entries, ok := readUint16(frame, &offset)
	if !ok {
		return VBRI{}, true, ErrIndexTooShort
	}
	scale, ok := readUint16(frame, &offset)
	if !ok {
		return VBRI{}, true, ErrIndexTooShort
	}
	entrySize, ok := readUint16(frame, &offset)
	if !ok {
		return VBRI{}, true, ErrIndexTooShort
	}
	framesPerEntry, ok := readUint16(frame, &offset)
	if !ok {
		return VBRI{}, true, ErrIndexTooShort
	}
	if version != 1 {
		return VBRI{}, true, ErrIndexInvalid
	}
	if entrySize < 1 || entrySize > 4 {
		return VBRI{}, true, ErrIndexInvalid
	}

	tocBytes := uint64(entries) * uint64(entrySize)
	if tocBytes > uint64(len(frame)-offset) {
		return VBRI{}, true, ErrIndexTooShort
	}
	toc := make([]uint32, entries)
	for i := range toc {
		value, next := readTOCEntry(frame[offset:], entrySize)
		offset += next
		toc[i] = value
	}

	return VBRI{
		version:        version,
		delay:          delay,
		quality:        quality,
		bytes:          bytes,
		frames:         frames,
		entries:        entries,
		scale:          scale,
		entrySize:      entrySize,
		framesPerEntry: framesPerEntry,
		toc:            toc,
	}, true, nil
}

func readUint16(data []byte, offset *int) (uint16, bool) {
	if *offset < 0 || *offset > len(data) || len(data)-*offset < 2 {
		return 0, false
	}
	value := binary.BigEndian.Uint16(data[*offset : *offset+2])
	*offset += 2
	return value, true
}

func readTOCEntry(data []byte, entrySize uint16) (uint32, int) {
	size := int(entrySize)
	switch entrySize {
	case 1:
		return uint32(data[0]), size
	case 2:
		return uint32(binary.BigEndian.Uint16(data[:2])), size
	case 3:
		return uint32(data[0])<<16 | uint32(data[1])<<8 | uint32(data[2]), size
	case 4:
		return binary.BigEndian.Uint32(data[:4]), size
	default:
		panic("invalid VBRI entry size")
	}
}
