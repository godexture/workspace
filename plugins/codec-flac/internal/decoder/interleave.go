package decoder

import "encoding/binary"

func interleaveS8(plane []byte, samples [][]int64, blockSize, channels int) {
	for sample := 0; sample < blockSize; sample++ {
		for channel := 0; channel < channels; channel++ {
			offset := sample*channels + channel
			plane[offset] = byte(int8(samples[channel][sample]))
		}
	}
}

func interleaveS16(plane []byte, samples [][]int64, blockSize, channels int) {
	for sample := 0; sample < blockSize; sample++ {
		for channel := 0; channel < channels; channel++ {
			offset := (sample*channels + channel) * 2
			binary.LittleEndian.PutUint16(plane[offset:], uint16(int16(samples[channel][sample])))
		}
	}
}

func interleaveS24(plane []byte, samples [][]int64, blockSize, channels int) {
	for sample := 0; sample < blockSize; sample++ {
		for channel := 0; channel < channels; channel++ {
			offset := (sample*channels + channel) * 3
			value := samples[channel][sample]
			plane[offset] = byte(value)
			plane[offset+1] = byte(value >> 8)
			plane[offset+2] = byte(value >> 16)
		}
	}
}

func interleaveS32Scalar(plane []byte, samples [][]int64, blockSize, channels int) {
	for sample := 0; sample < blockSize; sample++ {
		for channel := 0; channel < channels; channel++ {
			offset := (sample*channels + channel) * 4
			binary.LittleEndian.PutUint32(plane[offset:], uint32(samples[channel][sample]))
		}
	}
}

func interleaveS32StereoScalar(plane []byte, left, right []int64, start, blockSize int) {
	for sample := start; sample < blockSize; sample++ {
		binary.LittleEndian.PutUint32(plane[sample*8:], uint32(left[sample]))
		binary.LittleEndian.PutUint32(plane[sample*8+4:], uint32(right[sample]))
	}
}
