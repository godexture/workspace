package frame

import "encoding/binary"

const (
	xingMPEG1StereoSideInfo = 32
	xingMPEG1MonoSideInfo   = 17
	xingMPEG2StereoSideInfo = 17
	xingMPEG2MonoSideInfo   = 9
	xingHeaderBytes         = 4
	xingMarkerBytes         = 4
	xingTOCBytes            = 100
)

// ParseXing parses a Xing or Info index from a complete frame prefix. The
// frame slice starts at the MPEG frame header. found is false when the marker
// is absent. A Layer I/II frame has no Layer III side-info placement and is
// treated as absent.
func ParseXing(header Header, frame []byte) (Xing, bool, error) {
	if !header.Valid() {
		return Xing{}, false, ErrInvalidHeader
	}
	if header.Layer() != LayerIII {
		return Xing{}, false, nil
	}

	offset := xingOffset(header)
	if len(frame) < offset+xingMarkerBytes {
		return Xing{}, false, nil
	}

	marker := frame[offset : offset+xingMarkerBytes]
	var kind XingKind
	switch string(marker) {
	case "Xing":
		kind = XingKindXing
	case "Info":
		kind = XingKindInfo
	default:
		return Xing{}, false, nil
	}
	offset += xingMarkerBytes

	flags, ok := readUint32(frame, &offset)
	if !ok {
		return Xing{}, true, ErrIndexTooShort
	}
	result := Xing{kind: kind, flags: flags}
	if flags&0x0001 != 0 {
		result.frames, ok = readUint32(frame, &offset)
		if !ok {
			return Xing{}, true, ErrIndexTooShort
		}
	}
	if flags&0x0002 != 0 {
		result.bytes, ok = readUint32(frame, &offset)
		if !ok {
			return Xing{}, true, ErrIndexTooShort
		}
	}
	if flags&0x0004 != 0 {
		if len(frame)-offset < xingTOCBytes {
			return Xing{}, true, ErrIndexTooShort
		}
		copy(result.toc[:], frame[offset:offset+xingTOCBytes])
		offset += xingTOCBytes
	}
	if flags&0x0008 != 0 {
		result.quality, ok = readUint32(frame, &offset)
		if !ok {
			return Xing{}, true, ErrIndexTooShort
		}
	}
	return result, true, nil
}

func xingOffset(header Header) int {
	// CRC protection does not move the Xing data in the first frame.
	offset := xingHeaderBytes
	mono := header.ChannelMode() == Mono
	if header.Version() == VersionMPEG1 {
		if mono {
			return offset + xingMPEG1MonoSideInfo
		}
		return offset + xingMPEG1StereoSideInfo
	}
	if mono {
		return offset + xingMPEG2MonoSideInfo
	}
	return offset + xingMPEG2StereoSideInfo
}

func readUint32(data []byte, offset *int) (uint32, bool) {
	if *offset < 0 || *offset > len(data) || len(data)-*offset < 4 {
		return 0, false
	}
	value := binary.BigEndian.Uint32(data[*offset : *offset+4])
	*offset += 4
	return value, true
}
