//go:generate go run generate.go
package g711

import "encoding/binary"

func DecodePCMU(data []byte, order binary.ByteOrder) []byte {
	out := make([]byte, len(data)<<1)
	for i := 0; i < len(data); i++ {
		order.PutUint16(out[i<<1:], uLawToLinearTable[data[i]])
	}
	return out
}

func DecodePCMA(data []byte, order binary.ByteOrder) []byte {
	out := make([]byte, len(data)<<1)
	for i := 0; i < len(data); i++ {
		order.PutUint16(out[i<<1:], aLawToLinearTable[data[i]])
	}
	return out
}

func EncodePCMU(data []byte, order binary.ByteOrder) []byte {
	out := make([]byte, len(data)>>1)
	for i := 0; i < len(out); i++ {
		out[i] = linearToULawTable[order.Uint16(data[i<<1:])]
	}
	return out
}

func EncodePCMA(data []byte, order binary.ByteOrder) []byte {
	out := make([]byte, len(data)>>1)
	for i := 0; i < len(out); i++ {
		out[i] = linearToALawTable[order.Uint16(data[i<<1:])]
	}
	return out
}
