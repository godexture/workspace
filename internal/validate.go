package internal

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"hash"
)

type hashState struct {
	hash   hash.Hash
	active bool
}

func (d *Decoder) initMD5() {
	if d.info.MD5 != [16]byte{} {
		d.md5Hash.hash = md5.New()
		d.md5Hash.active = true
	}
}

func (d *Decoder) validateFrame(header frameHeader) error {
	if d.frameCount == 0 {
		if header.number != 0 {
			return fmt.Errorf("invalid first FLAC frame number: %d", header.number)
		}
	} else if header.blockingStrategy {
		if header.number != d.sampleCount {
			return fmt.Errorf("unexpected FLAC sample number: got %d, want %d", header.number, d.sampleCount)
		}
	} else if header.number != d.frameCount && header.number != d.sampleCount {
		// A number of pre-subset encoders used sample numbers with the fixed
		// blocking bit clear. Accept that interoperable legacy form while still
		// rejecting unrelated/non-monotonic numbers.
		return fmt.Errorf("unexpected FLAC frame/sample number: got %d, want frame %d or sample %d", header.number, d.frameCount, d.sampleCount)
	}
	if d.info.MaxBlockSize > 0 && header.blockSize > int(d.info.MaxBlockSize) {
		return fmt.Errorf("FLAC frame block size %d exceeds STREAMINFO maximum %d", header.blockSize, d.info.MaxBlockSize)
	}
	d.frameCount++
	d.sampleCount += uint64(header.blockSize)
	return nil
}

func (d *Decoder) validateEnd() error {
	if d.info.TotalSamples > 0 && d.sampleCount != d.info.TotalSamples {
		return fmt.Errorf("FLAC sample count mismatch: got %d, want %d", d.sampleCount, d.info.TotalSamples)
	}
	if !d.md5Hash.active {
		return nil
	}
	var got [16]byte
	copy(got[:], d.md5Hash.hash.Sum(nil))
	if got != d.info.MD5 {
		return fmt.Errorf("FLAC PCM MD5 mismatch: got %x, want %x", got, d.info.MD5)
	}
	return nil
}

func (d *Decoder) updateMD5(decoded decodedFrame) {
	if !d.md5Hash.active {
		return
	}
	width := (decoded.header.bitsPerSample + 7) / 8
	var sample [4]byte
	for i := 0; i < decoded.header.blockSize; i++ {
		for ch := 0; ch < decoded.header.channels; ch++ {
			value := decoded.samples[ch][i]
			binary.LittleEndian.PutUint32(sample[:], uint32(value))
			d.md5Hash.hash.Write(sample[:width])
		}
	}
}
