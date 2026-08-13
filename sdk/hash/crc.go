//go:generate go run generate.go

// Package hash provides CRC and FNV checksums for codec implementations.
//
// It has no consumer in the current tree: the codecs that used it are still
// in _legacy pending the M8 family migration, which also decides whether this
// stays a public package. Treat the API as unstable until then.
package hash

// CRC8 computes the FLAC frame header checksum (polynomial 0x07, no
// reflection, zero init).
func CRC8(data []byte) byte {
	var crc byte
	for _, value := range data {
		crc = crc8Table[crc^value]
	}
	return crc
}

// CRC16 computes the FLAC frame footer checksum (polynomial 0x8005, no
// reflection, zero init).
func CRC16(data []byte) uint16 {
	return CRC16Update(0, data)
}

type crc16Slice8Tables struct {
	byteTable [8][256]uint16
	stateHi   [256]uint16
	stateLo   [256]uint16
}

// CRC16Update extends a FLAC frame CRC with data. The initial CRC is zero.
func CRC16Update(crc uint16, data []byte) uint16 {
	for len(data) >= 8 {
		block := data[:8:8]
		crc = crc16Slice8.stateHi[crc>>8] ^ crc16Slice8.stateLo[byte(crc)] ^
			crc16Slice8.byteTable[0][block[0]] ^
			crc16Slice8.byteTable[1][block[1]] ^
			crc16Slice8.byteTable[2][block[2]] ^
			crc16Slice8.byteTable[3][block[3]] ^
			crc16Slice8.byteTable[4][block[4]] ^
			crc16Slice8.byteTable[5][block[5]] ^
			crc16Slice8.byteTable[6][block[6]] ^
			crc16Slice8.byteTable[7][block[7]]
		data = data[8:]
	}
	for _, value := range data {
		crc = crc<<8 ^ crc16Table[byte(crc>>8)^value]
	}
	return crc
}
