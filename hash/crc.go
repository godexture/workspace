package hash

// crc8Table and crc16Table hold the per-byte results of the bit-serial CRC
// loop below, precomputed once so CRC8/CRC16 do 8 shift-XORs per input byte
// instead of 8 per input *bit*.
var (
	crc8Table  = buildCRC8Table()
	crc16Table = buildCRC16Table()
)

func buildCRC8Table() [256]byte {
	var table [256]byte
	for i := range table {
		crc := byte(i)
		for j := 0; j < 8; j++ {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x07
			} else {
				crc <<= 1
			}
		}
		table[i] = crc
	}
	return table
}

func buildCRC16Table() [256]uint16 {
	var table [256]uint16
	for i := range table {
		crc := uint16(i) << 8
		for j := 0; j < 8; j++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x8005
			} else {
				crc <<= 1
			}
		}
		table[i] = crc
	}
	return table
}

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

// CRC16Update extends a FLAC frame CRC with data. The initial CRC is zero.
func CRC16Update(crc uint16, data []byte) uint16 {
	for _, value := range data {
		crc = crc<<8 ^ crc16Table[byte(crc>>8)^value]
	}
	return crc
}
