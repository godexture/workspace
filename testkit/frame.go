package testkit

import (
	"errors"
	"unsafe"

	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/timing"
)

// Frame is the logical, ownership-free representation of a planar frame in one
// of the four canonical decoded representations.
type Frame[S audio.Sample] struct {
	PTS    timing.OptionalPTS
	Planes [][]S
}

// FrameInput builds planar frames. description must be the planar description
// whose canonical coding stores S, with one plane per channel.
func FrameInput[S audio.Sample](description sample.Description, values []Frame[S], options ...StreamOption) Fixture[audio.Frame[S]] {
	frameSchema := sample.Frames[S]()
	if !frameSchema.Valid() || !frameDescription(description, frameSchema.Identity().String()) {
		return Fixture[audio.Frame[S]]{}
	}
	descriptor, err := mediaDescriptor(frameSchema.Descriptor(), description, options...)
	if err != nil {
		return Fixture[audio.Frame[S]]{}
	}
	allocator, err := buffer.NewAllocator(framePayloadLimit(values))
	if err != nil {
		return Fixture[audio.Frame[S]]{}
	}
	items := make([]audio.Frame[S], 0, len(values))
	for _, value := range values {
		frame, allocationErr := allocateFrame(allocator, value)
		if allocationErr != nil {
			releaseFrames(items)
			return Fixture[audio.Frame[S]]{}
		}
		items = append(items, frame)
	}
	result := Values(descriptor, frameSchema, items...)
	releaseFrames(items)
	result.verify = allocatorVerifier(allocator)
	return result
}

// WantFrames compares planar samples and timestamps in order.
func WantFrames[S audio.Sample](want ...Frame[S]) Expectation[audio.Frame[S]] {
	return WantValues(cloneFrames(want), snapshotFrame[S])
}

// frameDescription reports whether description names the planar stream that S
// stores, so a fixture cannot pair int16 planes with a float32 descriptor.
func frameDescription(description sample.Description, identity string) bool {
	if description.Packing != sample.Planar || description.Endian != sample.NoEndian {
		return false
	}
	canonical, ok := sample.Schema(description.Coding)
	return ok && canonical.Identity().String() == identity
}

func framePayloadLimit[S audio.Sample](values []Frame[S]) int64 {
	size := int64(sampleBytes[S]())
	var total int64
	for _, value := range values {
		for _, plane := range value.Planes {
			total += int64(len(plane)) * size
		}
	}
	if total == 0 {
		return 1
	}
	return total + int64(len(values))*32
}

func allocateFrame[S audio.Sample](allocator *buffer.Allocator, value Frame[S]) (audio.Frame[S], error) {
	if len(value.Planes) == 0 {
		return audio.Frame[S]{}, errors.New("frame fixture has no planes")
	}
	size := sampleBytes[S]()
	samples := len(value.Planes[0])
	planes := make([]buffer.PlaneSpec, len(value.Planes))
	for index, values := range value.Planes {
		if len(values) != samples {
			return audio.Frame[S]{}, errors.New("frame fixture planes have different sample counts")
		}
		planes[index].Size = len(values) * size
	}
	lease, err := allocator.Overwrite(buffer.Spec{Alignment: 16, Planes: planes})
	if err != nil {
		return audio.Frame[S]{}, err
	}
	defer lease.Discard()
	if err := lease.Fill(func(storage buffer.Mutable) error {
		for index, values := range value.Planes {
			plane, planeErr := storage.Plane(index)
			if planeErr != nil {
				return planeErr
			}
			target, castErr := audio.Plane[S](plane, samples)
			if castErr != nil {
				return castErr
			}
			copy(target, values)
		}
		return nil
	}); err != nil {
		return audio.Frame[S]{}, err
	}
	handle, err := lease.Commit()
	if err != nil {
		return audio.Frame[S]{}, err
	}
	frame, err := audio.NewFrame[S](value.PTS, samples, handle)
	if err != nil {
		handle.Release()
		return audio.Frame[S]{}, err
	}
	return frame, nil
}

func snapshotFrame[S audio.Sample](value audio.Frame[S]) (Frame[S], error) {
	if !value.Valid() {
		return Frame[S]{}, errors.New("invalid frame")
	}
	planes := value.Planes().Layout().Planes
	result := Frame[S]{PTS: value.PTS(), Planes: make([][]S, len(planes))}
	for index := range planes {
		values, err := value.PlaneSamples(index)
		if err != nil {
			return Frame[S]{}, err
		}
		result.Planes[index] = values.AppendTo(nil)
	}
	return result, nil
}

func cloneFrames[S audio.Sample](values []Frame[S]) []Frame[S] {
	result := append([]Frame[S](nil), values...)
	for index := range result {
		result[index].Planes = make([][]S, len(values[index].Planes))
		for plane := range result[index].Planes {
			result[index].Planes[plane] = append([]S(nil), values[index].Planes[plane]...)
		}
	}
	return result
}

func releaseFrames[S audio.Sample](values []audio.Frame[S]) {
	for _, value := range values {
		value.Release()
	}
}

func sampleBytes[S audio.Sample]() int { return int(unsafe.Sizeof(*new(S))) }
