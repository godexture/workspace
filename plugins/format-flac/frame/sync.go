package frame

import "bytes"

func nextSyncPrefix(data []byte, start int) int {
	start = max(start, 0)
	for start+1 < len(data) {
		offset := bytes.IndexByte(data[start:len(data)-1], 0xff)
		if offset < 0 {
			return -1
		}
		position := start + offset
		if data[position+1]&0xfc == 0xf8 {
			return position
		}
		start = position + 1
	}
	return -1
}
