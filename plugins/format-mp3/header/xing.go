package header

import (
	"encoding/binary"
)

type XingHeader struct {
	Frames    uint32
	Bytes     uint32
	TOC       [100]byte
	Quality   uint32
	HasFrames bool
	HasBytes  bool
	HasTOC    bool
}

func ParseXingHeader(frameData []byte, isMPEG1 bool, isMono bool) (*XingHeader, error) {
	offset := 21
	if isMPEG1 {
		if isMono {
			offset = 21
		} else {
			offset = 36
		}
	} else {
		if isMono {
			offset = 13
		} else {
			offset = 21
		}
	}

	if len(frameData) < offset+4 {
		return nil, nil // Too short to contain Xing header
	}

	magic := string(frameData[offset : offset+4])
	if magic != "Xing" && magic != "Info" {
		return nil, nil // Not a Xing/Info header
	}

	offset += 4
	if len(frameData) < offset+4 {
		return nil, nil
	}

	flags := binary.BigEndian.Uint32(frameData[offset : offset+4])
	offset += 4

	xh := &XingHeader{}
	if flags&0x0001 != 0 {
		if len(frameData) < offset+4 {
			return nil, nil
		}
		xh.Frames = binary.BigEndian.Uint32(frameData[offset : offset+4])
		xh.HasFrames = true
		offset += 4
	}
	if flags&0x0002 != 0 {
		if len(frameData) < offset+4 {
			return nil, nil
		}
		xh.Bytes = binary.BigEndian.Uint32(frameData[offset : offset+4])
		xh.HasBytes = true
		offset += 4
	}
	if flags&0x0004 != 0 {
		if len(frameData) < offset+100 {
			return nil, nil
		}
		copy(xh.TOC[:], frameData[offset:offset+100])
		xh.HasTOC = true
		offset += 100
	}
	if flags&0x0008 != 0 {
		if len(frameData) < offset+4 {
			return nil, nil
		}
		xh.Quality = binary.BigEndian.Uint32(frameData[offset : offset+4])
		offset += 4
	}

	return xh, nil
}
