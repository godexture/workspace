package standard_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/standard"
)

// TestConvertStreamsInputLargerThanTheSourceGrant converts far more bytes than
// any single component is granted. It fails if payload memory is charged
// against a bound that the running pipeline can exceed: the source then stalls
// on its own grant partway through the file rather than at end of input, so
// the defect appears only once the input outgrows the grant.
func TestConvertStreamsInputLargerThanTheSourceGrant(t *testing.T) {
	for _, megabytes := range []int{1, 4, 16} {
		source, payload := linearWave(megabytes * 1024 * 1024 / 4)
		directory := t.TempDir()
		input := filepath.Join(directory, "input.wav")
		output := filepath.Join(directory, "output.wav")
		if err := os.WriteFile(input, source, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := standard.Convert(context.Background(), input, output); err != nil {
			t.Errorf("%d MiB: %v", megabytes, err)
			continue
		}
		converted, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(converted, payload) {
			t.Errorf("%d MiB: converted %d bytes do not carry the input payload", megabytes, len(converted))
		}
	}
}

// linearWave builds a 48 kHz stereo signed 16-bit WAVE whose payload is a
// distinct byte pattern, and returns the file and that payload.
func linearWave(frames int) (file, payload []byte) {
	payload = make([]byte, frames*4)
	for index := range payload {
		payload[index] = byte(index * 7)
	}
	format := make([]byte, 16)
	binary.LittleEndian.PutUint16(format[0:2], 1)
	binary.LittleEndian.PutUint16(format[2:4], 2)
	binary.LittleEndian.PutUint32(format[4:8], 48_000)
	binary.LittleEndian.PutUint32(format[8:12], 48_000*4)
	binary.LittleEndian.PutUint16(format[12:14], 4)
	binary.LittleEndian.PutUint16(format[14:16], 16)

	body := append([]byte("WAVE"), []byte("fmt ")...)
	body = append(body, sizeOf(len(format))...)
	body = append(body, format...)
	body = append(body, []byte("data")...)
	body = append(body, sizeOf(len(payload))...)
	body = append(body, payload...)

	file = append([]byte("RIFF"), sizeOf(len(body))...)
	return append(file, body...), payload
}

func sizeOf(value int) []byte {
	result := make([]byte, 4)
	binary.LittleEndian.PutUint32(result, uint32(value))
	return result
}
