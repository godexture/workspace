package dsp

import "unsafe"

type Sample interface {
	~int16 | ~int32 | ~float32
}

func AsSamples[T Sample](data []byte) []T {
	if len(data) == 0 {
		return nil
	}
	size := int(unsafe.Sizeof(*new(T)))
	if uintptr(unsafe.Pointer(&data[0]))%uintptr(size) != 0 {
		return nil
	}
	return unsafe.Slice((*T)(unsafe.Pointer(&data[0])), len(data)/size)
}
