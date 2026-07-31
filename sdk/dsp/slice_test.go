package dsp

import (
	"testing"
	"unsafe"
)

func TestAsSamples(t *testing.T) {
	data := make([]byte, 16)
	samples := AsSamples[int32](data)
	if len(samples) != 4 {
		t.Fatalf("len = %d, want 4", len(samples))
	}
	samples[1] = 0x12345678
	if *(*int32)(unsafe.Pointer(&data[4])) != 0x12345678 {
		t.Fatal("sample slice does not share storage")
	}
}

func TestAsSamplesRejectsMisalignment(t *testing.T) {
	data := make([]byte, 17)
	if got := AsSamples[int32](data[1:]); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}
