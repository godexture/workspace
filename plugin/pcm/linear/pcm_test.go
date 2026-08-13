package linear

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/timing"
)

func TestPCMBlockConversionCrossesScratchBoundary(t *testing.T) {
	for _, test := range []struct {
		name       string
		channels   int
		shift      uint
		little     bool
		extraFrame int
	}{
		{name: "mono-little-s16", channels: 1, little: true, extraFrame: 17},
		{name: "mono-big-valid12", channels: 1, shift: 4, extraFrame: 19},
		{name: "stereo-little-s16", channels: 2, little: true, extraFrame: 23},
		{name: "stereo-big-valid13", channels: 2, shift: 3, extraFrame: 29},
	} {
		t.Run(test.name, func(t *testing.T) {
			frameBytes := test.channels * 2
			samples := pcmBlockBytes/frameBytes + test.extraFrame
			planes := make([][]int16, test.channels)
			for channel := range planes {
				planes[channel] = make([]int16, samples)
			}
			encoded := make([]byte, samples*frameBytes)
			validBits := 16 - int(test.shift)
			rangeSize := 1 << validBits
			minimum := -(rangeSize / 2)
			for index := range samples {
				for channel := 0; channel < test.channels; channel++ {
					value := int16(minimum + (index*37+channel*101)%rangeSize)
					planes[channel][index] = value
					offset := (index*test.channels + channel) * 2
					wire := uint16(value) << test.shift
					if test.little {
						binary.LittleEndian.PutUint16(encoded[offset:], wire)
					} else {
						binary.BigEndian.PutUint16(encoded[offset:], wire)
					}
				}
			}

			allocator, err := buffer.NewAllocator(int64(len(encoded)*3 + 64))
			if err != nil {
				t.Fatal(err)
			}
			input, err := allocator.FromBytes(encoded, 2)
			if err != nil {
				t.Fatal(err)
			}
			destinations := [2][]byte{}
			for channel := 0; channel < test.channels; channel++ {
				destinations[channel] = make([]byte, samples*2)
			}
			scratch := make([]byte, pcmBlockBytes)
			if err := decodePCM(input.Bytes(), scratch, destinations, test.channels, samples, test.shift, test.little); err != nil {
				input.Release()
				t.Fatal(err)
			}
			for channel := 0; channel < test.channels; channel++ {
				for index, want := range planes[channel] {
					got := int16(binary.NativeEndian.Uint16(destinations[channel][index*2:]))
					if got != want {
						input.Release()
						t.Fatalf("plane %d sample %d = %d, want %d", channel, index, got, want)
					}
				}
			}

			specs := make([]buffer.PlaneSpec, test.channels)
			for channel := range specs {
				specs[channel].Size = samples * 2
			}
			lease, err := allocator.Overwrite(buffer.Spec{Alignment: 16, Planes: specs})
			if err != nil {
				input.Release()
				t.Fatal(err)
			}
			if err := lease.Fill(func(storage buffer.Mutable) error {
				for channel := 0; channel < test.channels; channel++ {
					plane, planeErr := storage.Plane(channel)
					if planeErr != nil {
						return planeErr
					}
					copy(plane, destinations[channel])
				}
				return nil
			}); err != nil {
				input.Release()
				t.Fatal(err)
			}
			frameHandle, err := lease.Commit()
			if err != nil {
				input.Release()
				t.Fatal(err)
			}
			frame, err := audio.NewFrame[int16](timing.UnknownPTS(), samples, frameHandle)
			if err != nil {
				frameHandle.Release()
				input.Release()
				t.Fatal(err)
			}
			output := make([]byte, len(encoded))
			if err := encodePCM(frame, make([]int16, pcmBlockBytes/2), output, test.channels, test.shift, test.little); err != nil {
				frame.Release()
				input.Release()
				t.Fatal(err)
			}
			if !input.Bytes().EqualSlice(output) {
				frame.Release()
				input.Release()
				t.Fatal("block conversion changed PCM bytes")
			}
			frame.Release()
			input.Release()
			if allocator.Used() != 0 {
				t.Fatalf("block conversion retained %d bytes", allocator.Used())
			}
		})
	}
}

func TestDecodePCMRejectsUnsupportedChannelsAndShortPlanes(t *testing.T) {
	allocator, err := buffer.NewAllocator(8)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := allocator.FromBytes(make([]byte, 8), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Release()
	scratch := make([]byte, pcmBlockBytes)
	if err := decodePCM(handle.Bytes(), scratch, [2][]byte{}, 3, 1, 0, true); !errors.Is(err, ErrPlaneCount) {
		t.Fatalf("unsupported channels error = %v", err)
	}
	if err := decodePCM(handle.Bytes(), scratch, [2][]byte{{}}, 1, 1, 0, true); !errors.Is(err, audio.ErrInvalidPlanes) {
		t.Fatalf("short plane error = %v", err)
	}
	if err := decodePCM(handle.Bytes(), scratch[:1], [2][]byte{make([]byte, 2)}, 1, 1, 0, true); !errors.Is(err, ErrPartialSample) {
		t.Fatalf("undersized decode scratch error = %v", err)
	}

	frame, err := audio.NewFrame[int16](timing.UnknownPTS(), 4, handle.Share())
	if err != nil {
		t.Fatal(err)
	}
	defer frame.Release()
	if err := encodePCM(frame, nil, make([]byte, 8), 1, 0, true); !errors.Is(err, ErrPartialSample) {
		t.Fatalf("empty encode scratch error = %v", err)
	}
}
