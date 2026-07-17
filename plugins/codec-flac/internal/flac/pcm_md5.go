package flac

import (
	"crypto/md5"
	"hash"
)

type PCMMD5 struct {
	hash    hash.Hash
	scratch []byte
}

func NewPCMMD5() *PCMMD5 {
	return &PCMMD5{hash: md5.New()}
}

func (m *PCMMD5) Write(samples [][]int64, bitsPerSample int) {
	m.scratch = PackPCMMD5(m.scratch, samples, bitsPerSample)
	m.hash.Write(m.scratch)
}

func (m *PCMMD5) WritePacked(samples []byte) {
	m.hash.Write(samples)
}

func (m *PCMMD5) Sum() [16]byte {
	var sum [16]byte
	copy(sum[:], m.hash.Sum(nil))
	return sum
}

// PackPCMMD5 serializes planar PCM samples using FLAC's STREAMINFO MD5 form.
func PackPCMMD5(scratch []byte, samples [][]int64, bitsPerSample int) []byte {
	if len(samples) == 0 || len(samples[0]) == 0 {
		return scratch[:0]
	}
	width := (bitsPerSample + 7) / 8
	needed := len(samples[0]) * len(samples) * width
	if cap(scratch) < needed+4 {
		scratch = make([]byte, needed+4)
	}
	data := scratch[:needed+4]
	switch width {
	case 1:
		packPCMMD5Width1(data, samples)
	case 2:
		packPCMMD5Width2(data, samples)
	case 3:
		packPCMMD5Width3(data, samples)
	default:
		packPCMMD5Width4(data, samples)
	}
	return data[:needed]
}

func packPCMMD5Width1(data []byte, samples [][]int64) {
	offset := 0
	for sample := range samples[0] {
		for channel := range samples {
			data[offset] = byte(samples[channel][sample])
			offset++
		}
	}
}

func packPCMMD5Width2(data []byte, samples [][]int64) {
	offset := 0
	for sample := range samples[0] {
		for channel := range samples {
			value := uint32(samples[channel][sample])
			data[offset], data[offset+1] = byte(value), byte(value>>8)
			offset += 2
		}
	}
}

func packPCMMD5Width3(data []byte, samples [][]int64) {
	offset := 0
	for sample := range samples[0] {
		for channel := range samples {
			value := uint32(samples[channel][sample])
			data[offset], data[offset+1], data[offset+2] = byte(value), byte(value>>8), byte(value>>16)
			offset += 3
		}
	}
}

func packPCMMD5Width4(data []byte, samples [][]int64) {
	offset := 0
	for sample := range samples[0] {
		for channel := range samples {
			value := uint32(samples[channel][sample])
			data[offset], data[offset+1], data[offset+2], data[offset+3] = byte(value), byte(value>>8), byte(value>>16), byte(value>>24)
			offset += 4
		}
	}
}
