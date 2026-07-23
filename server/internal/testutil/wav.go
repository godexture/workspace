// Package testutil provides small fixtures shared by example-web's tests.
package testutil

import (
	"encoding/binary"
	"io"
	"math"
)

// WriteWAV writes a short synthetic mono 16-bit PCM WAV (a 440Hz tone) so
// tests do not depend on the large shared fixture assets used elsewhere in
// the repo.
func WriteWAV(w io.Writer) error {
	const sampleRate = 8000
	const numSamples = 4000
	samples := make([]int16, numSamples)
	for i := range samples {
		samples[i] = int16(4000 * math.Sin(2*math.Pi*440*float64(i)/sampleRate))
	}
	dataSize := uint32(len(samples) * 2)

	fields := []any{
		[]byte("RIFF"), uint32(36 + dataSize), []byte("WAVE"),
		[]byte("fmt "), uint32(16), uint16(1), uint16(1), uint32(sampleRate), uint32(sampleRate * 2), uint16(2), uint16(16),
		[]byte("data"), dataSize, samples,
	}
	for _, field := range fields {
		if b, ok := field.([]byte); ok {
			if _, err := w.Write(b); err != nil {
				return err
			}
			continue
		}
		if err := binary.Write(w, binary.LittleEndian, field); err != nil {
			return err
		}
	}
	return nil
}
