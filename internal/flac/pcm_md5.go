package flac

import (
	"crypto/md5"
	"encoding/binary"
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
	offset := 0
	for sample := range samples[0] {
		for channel := range samples {
			binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(samples[channel][sample]))
			offset += width
		}
	}
	return data[:needed]
}
