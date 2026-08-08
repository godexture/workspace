package id3v2

func DecodeSyncSafeInt(buffer []byte) int {
	return (int(buffer[0]) << 21) | (int(buffer[1]) << 14) | (int(buffer[2]) << 7) | int(buffer[3])
}

func EncodeSyncSafeInt(value int) []byte {
	return []byte{
		byte((value >> 21) & 0x7F),
		byte((value >> 14) & 0x7F),
		byte((value >> 7) & 0x7F),
		byte(value & 0x7F),
	}
}
