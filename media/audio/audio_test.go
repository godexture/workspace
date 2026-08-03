package audio

import (
	"testing"

	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/timing"
)

func TestFrameValidatesPlanarPlanesAtConstruction(t *testing.T) {
	planes, err := buffer.Allocate(buffer.Spec{Alignment: 16, Planes: []buffer.PlaneSpec{{Size: 6}, {Size: 6}}})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := NewFrame[int16](timing.SomePTS(timing.NewPTS(0)), 3, planes)
	if err != nil {
		t.Fatal(err)
	}
	defer frame.Release()
	if !frame.Valid() || frame.Samples() != 3 || !frame.PTS().Valid() || frame.PTS().Value() != 0 {
		t.Fatalf("frame = %#v", frame)
	}
	if len(frame.Planes().Bytes()) == 0 {
		t.Fatal("frame did not retain backing planes")
	}
}

func TestFrameRejectsShortPlane(t *testing.T) {
	planes, err := buffer.Allocate(buffer.Spec{Planes: []buffer.PlaneSpec{{Size: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	defer planes.Release()
	if _, err := NewFrame[int16](timing.UnknownPTS(), 2, planes); err != ErrInvalidPlanes {
		t.Fatalf("short plane error = %v", err)
	}
}
