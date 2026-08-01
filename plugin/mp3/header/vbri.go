package header

import (
	"encoding/binary"
)

type VBRIHeader struct {
	Version        uint16
	Delay          uint16
	Quality        uint16
	Bytes          uint32
	Frames         uint32
	TOCEntries     uint16
	Scale          uint32
	EntrySize      uint16
	FramesPerEntry uint16
	TOC            []uint32 // Accumulated offsets
}

func ParseVBRIHeader(frameData []byte) (*VBRIHeader, error) {
	offset := 36
	if len(frameData) < offset+26 {
		return nil, nil
	}

	magic := string(frameData[offset : offset+4])
	if magic != "VBRI" {
		return nil, nil
	}

	vh := &VBRIHeader{
		Version:        binary.BigEndian.Uint16(frameData[offset+4 : offset+6]),
		Delay:          binary.BigEndian.Uint16(frameData[offset+6 : offset+8]),
		Quality:        binary.BigEndian.Uint16(frameData[offset+8 : offset+10]),
		Bytes:          binary.BigEndian.Uint32(frameData[offset+10 : offset+14]),
		Frames:         binary.BigEndian.Uint32(frameData[offset+14 : offset+18]),
		TOCEntries:     binary.BigEndian.Uint16(frameData[offset+18 : offset+20]),
		Scale:          uint32(binary.BigEndian.Uint16(frameData[offset+20 : offset+22])),
		EntrySize:      binary.BigEndian.Uint16(frameData[offset+22 : offset+24]),
		FramesPerEntry: binary.BigEndian.Uint16(frameData[offset+24 : offset+26]),
	}

	tocStart := offset + 26
	tocBytes := int(vh.TOCEntries) * int(vh.EntrySize)
	if len(frameData) < tocStart+tocBytes {
		return nil, nil
	}

	vh.TOC = make([]uint32, vh.TOCEntries)
	var accumulated uint32 = 0
	curr := tocStart
	for i := 0; i < int(vh.TOCEntries); i++ {
		var val uint32
		switch vh.EntrySize {
		case 1:
			val = uint32(frameData[curr])
			curr += 1
		case 2:
			val = uint32(binary.BigEndian.Uint16(frameData[curr : curr+2]))
			curr += 2
		case 3:
			val = uint32(frameData[curr])<<16 | uint32(frameData[curr+1])<<8 | uint32(frameData[curr+2])
			curr += 3
		case 4:
			val = binary.BigEndian.Uint32(frameData[curr : curr+4])
			curr += 4
		default:
			return nil, nil // Invalid entry size
		}
		accumulated += val * vh.Scale
		vh.TOC[i] = accumulated
	}

	return vh, nil
}
