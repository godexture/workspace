package internal

import "encoding/binary"

func leftJustifyS16Scalar(destination, source []byte, shift uint) {
	length := min(len(destination), len(source))
	for i := 0; i+2 <= length; i += 2 {
		value := int16(binary.LittleEndian.Uint16(source[i:i+2])) << shift
		binary.LittleEndian.PutUint16(destination[i:i+2], uint16(value))
	}
}

func leftJustifyS32Scalar(destination, source []byte, shift uint) {
	length := min(len(destination), len(source))
	for i := 0; i+4 <= length; i += 4 {
		value := int32(binary.LittleEndian.Uint32(source[i:i+4])) << shift
		binary.LittleEndian.PutUint32(destination[i:i+4], uint32(value))
	}
}
