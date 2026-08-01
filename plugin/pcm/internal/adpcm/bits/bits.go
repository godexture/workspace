package bits

import "encoding/binary"

func WriteS16(buf []byte, off int, val int16, byteOrder binary.ByteOrder) {
	byteOrder.PutUint16(buf[off:off+2], uint16(val))
}

func BytesToS16(buf []byte, byteOrder binary.ByteOrder) []int16 {
	res := make([]int16, len(buf)/2)
	for i := 0; i < len(res); i++ {
		res[i] = int16(byteOrder.Uint16(buf[i*2 : i*2+2]))
	}
	return res
}
