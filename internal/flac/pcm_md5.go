package flac

import (
	"crypto/md5"
	"hash"

	"github.com/godexture/sdk/dsp"
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

// pcmMD5Kinds maps a container byte width to the pkg/dsp integer PCM kind
// that packs it. FLAC's STREAMINFO MD5 is always over its native signed
// samples regardless of bit depth, so this uses the signed kind at every
// width (U8's WAV-style bias never applies here).
var pcmMD5Kinds = map[int]dsp.PCMKind{1: dsp.PCMS8, 2: dsp.PCMS16, 3: dsp.PCMS24, 4: dsp.PCMS32}

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
	data := scratch[:needed]
	_ = dsp.FromInt64(data, samples, pcmMD5Kinds[width], len(samples[0]), len(samples))
	return data
}
