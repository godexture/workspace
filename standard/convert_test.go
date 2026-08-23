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
		source, payload := linearWave(megabytes*1024*1024/4, waveShape{channels: 2, bits: 16})
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

// A WAVE header states its coding and channel layout, and the conversion
// default is to keep both. These shapes were unreadable before the sample
// vocabulary separated coding from packing, so they are the ones most likely
// to regress into an unsupported-stream error.
func TestConvertKeepsEveryWAVEShapeItCanRead(t *testing.T) {
	for name, shape := range map[string]waveShape{
		"eight bit mono":       {channels: 1, bits: 8},
		"twenty four bit":      {channels: 2, bits: 24},
		"thirty two bit":       {channels: 2, bits: 32},
		"float":                {channels: 2, bits: 32, float: true},
		"double":               {channels: 1, bits: 64, float: true},
		"six channel surround": {channels: 6, bits: 24, mask: 0x3f},
		"three channel":        {channels: 3, bits: 16},
	} {
		t.Run(name, func(t *testing.T) {
			source, payload := linearWave(64, shape)
			directory := t.TempDir()
			input := filepath.Join(directory, "input.wav")
			output := filepath.Join(directory, "output.wav")
			if err := os.WriteFile(input, source, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := standard.Convert(context.Background(), input, output); err != nil {
				t.Fatal(err)
			}
			converted, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(converted, payload) {
				t.Fatalf("converted %d bytes do not carry the input payload", len(converted))
			}
			// The writer reserves a chunk the input did not have, so the file
			// is not byte-identical. What has to survive is the format header:
			// it is the only record of the coding and channel layout.
			format := waveFormat(shape, shape.channels*shape.bits/8)
			chunk := append(append([]byte("fmt "), sizeOf(len(format))...), format...)
			if !bytes.Contains(converted, chunk) {
				t.Fatalf("converted format header does not match the source:\n got %x\nwant %x", converted, chunk)
			}
		})
	}
}

type waveShape struct {
	channels int
	bits     int
	float    bool
	mask     uint32
}

// linearWave builds a 48 kHz WAVE of the requested shape whose payload is a
// distinct byte pattern, and returns the file and that payload.
func linearWave(frames int, shape waveShape) (file, payload []byte) {
	blockAlign := shape.channels * shape.bits / 8
	payload = make([]byte, frames*blockAlign)
	for index := range payload {
		payload[index] = byte(index * 7)
	}
	format := waveFormat(shape, blockAlign)

	body := append([]byte("WAVE"), []byte("fmt ")...)
	body = append(body, sizeOf(len(format))...)
	body = append(body, format...)
	body = append(body, []byte("data")...)
	body = append(body, sizeOf(len(payload))...)
	body = append(body, payload...)

	file = append([]byte("RIFF"), sizeOf(len(body))...)
	return append(file, body...), payload
}

func waveFormat(shape waveShape, blockAlign int) []byte {
	formatTag := uint16(1)
	if shape.float {
		formatTag = 3
	}
	size := 16
	if shape.mask != 0 {
		size = 40
	}
	value := make([]byte, size)
	header := formatTag
	if size == 40 {
		header = 0xfffe
	}
	binary.LittleEndian.PutUint16(value[0:2], header)
	binary.LittleEndian.PutUint16(value[2:4], uint16(shape.channels))
	binary.LittleEndian.PutUint32(value[4:8], 48_000)
	binary.LittleEndian.PutUint32(value[8:12], 48_000*uint32(blockAlign))
	binary.LittleEndian.PutUint16(value[12:14], uint16(blockAlign))
	binary.LittleEndian.PutUint16(value[14:16], uint16(shape.bits))
	if size == 40 {
		binary.LittleEndian.PutUint16(value[16:18], 22)
		binary.LittleEndian.PutUint16(value[18:20], uint16(shape.bits))
		binary.LittleEndian.PutUint32(value[20:24], shape.mask)
		binary.LittleEndian.PutUint16(value[24:26], formatTag)
		copy(value[28:40], []byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71})
	}
	return value
}

func sizeOf(value int) []byte {
	result := make([]byte, 4)
	binary.LittleEndian.PutUint32(result, uint32(value))
	return result
}

// waveFile wraps an explicit payload in the header its shape describes.
func waveFile(shape waveShape, payload []byte) []byte {
	format := waveFormat(shape, shape.channels*shape.bits/8)
	body := append([]byte("WAVE"), []byte("fmt ")...)
	body = append(body, sizeOf(len(format))...)
	body = append(body, format...)
	body = append(body, []byte("data")...)
	body = append(body, sizeOf(len(payload))...)
	body = append(body, payload...)
	return append(append([]byte("RIFF"), sizeOf(len(body))...), body...)
}
