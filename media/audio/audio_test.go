package audio

import (
	"testing"

	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/side"
	"github.com/godexture/godec/media/timing"
)

type frameSideKey struct{}

func frameAllocator(t *testing.T) *buffer.Allocator {
	t.Helper()
	allocator, err := buffer.NewAllocator(1024)
	if err != nil {
		t.Fatal(err)
	}
	return allocator
}

func TestFrameValidatesPlanarPlanesAtConstruction(t *testing.T) {
	planes, err := frameAllocator(t).Allocate(buffer.Spec{Alignment: 16, Planes: []buffer.PlaneSpec{{Size: 6}, {Size: 6}}})
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
	planes, err := frameAllocator(t).Allocate(buffer.Spec{Planes: []buffer.PlaneSpec{{Size: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	defer planes.Release()
	if _, err := NewFrame[int16](timing.UnknownPTS(), 2, planes); err != ErrInvalidPlanes {
		t.Fatalf("short plane error = %v", err)
	}
}

func TestFrameExposesValidatedTypedPlane(t *testing.T) {
	planes, err := frameAllocator(t).Allocate(buffer.Spec{Alignment: 16, Planes: []buffer.PlaneSpec{{Size: 6}}})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := NewFrame[int16](timing.UnknownPTS(), 3, planes)
	if err != nil {
		t.Fatal(err)
	}
	defer frame.Release()
	samples, err := frame.PlaneSamples(0)
	if err != nil {
		t.Fatal(err)
	}
	copy(samples, []int16{-32768, 0, 32767})
	again, err := frame.PlaneSamples(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 3 || again[0] != -32768 || again[2] != 32767 {
		t.Fatalf("samples = %v", again)
	}
}

func TestFramePlanesAreBorrowed(t *testing.T) {
	planes, err := frameAllocator(t).Allocate(buffer.Spec{Planes: []buffer.PlaneSpec{{Size: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := NewFrame[int16](timing.UnknownPTS(), 1, planes)
	if err != nil {
		t.Fatal(err)
	}
	view := frame.Planes()
	frame.Release()
	if view.Valid() {
		t.Fatal("frame planes accessor retained storage implicitly")
	}
}

func TestFrameCarriesSideDataWithoutChangingPlaneOwnership(t *testing.T) {
	planes, err := frameAllocator(t).Allocate(buffer.Spec{Planes: []buffer.PlaneSpec{{Size: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := NewFrame[int16](timing.UnknownPTS(), 1, planes)
	if err != nil {
		t.Fatal(err)
	}
	defer frame.Release()
	key := key.Define[frameSideKey, string]()
	data, err := side.Add(side.Data{}, key, "marker")
	if err != nil {
		t.Fatal(err)
	}
	withSide := frame.WithSideData(data)
	if value, ok := side.First(withSide.SideData(), key); !ok || value != "marker" {
		t.Fatalf("frame side data = %q, %v", value, ok)
	}
}

func TestExclusiveFrameEditReusesBackingWithoutAllocation(t *testing.T) {
	allocator := frameAllocator(t)
	planes, err := allocator.FromBytes([]byte{0, 0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := NewFrame[int16](timing.UnknownPTS(), 1, planes)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := frame.PlaneSamples(0)
	beforeAddress := &before[0]
	allocations := testing.AllocsPerRun(1000, func() {
		edit, err := frame.Edit(nil)
		if err != nil {
			panic(err)
		}
		if edit.Copied() {
			panic("exclusive edit copied")
		}
		candidate := edit.Frame()
		samples, err := edit.PlaneSamples(0)
		if err != nil {
			panic(err)
		}
		samples[0]++
		if err := edit.Commit(); err != nil {
			panic(err)
		}
		frame = candidate
	})
	if allocations != 0 {
		t.Fatalf("exclusive frame edit allocations = %v", allocations)
	}
	after, _ := frame.PlaneSamples(0)
	if &after[0] != beforeAddress || after[0] != 1001 {
		t.Fatalf("exclusive edit = address %p want %p, sample %d", &after[0], beforeAddress, after[0])
	}
	frame.Release()
	if allocator.Used() != 0 {
		t.Fatalf("exclusive edit retained %d bytes", allocator.Used())
	}
}

func TestSharedFrameEditCopiesOnlyCandidate(t *testing.T) {
	allocator := frameAllocator(t)
	planes, err := allocator.FromBytes([]byte{0, 0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := NewFrame[int16](timing.UnknownPTS(), 1, planes)
	if err != nil {
		t.Fatal(err)
	}
	shared := frame.Share()
	edit, err := frame.Edit(allocator)
	if err != nil {
		t.Fatal(err)
	}
	defer edit.Discard()
	if !edit.Copied() {
		t.Fatal("shared frame edit did not copy")
	}
	candidate := edit.Frame()
	samples, err := edit.PlaneSamples(0)
	if err != nil {
		t.Fatal(err)
	}
	samples[0] = 9
	sharedSamples, _ := shared.PlaneSamples(0)
	if sharedSamples[0] != 0 {
		t.Fatalf("shared branch changed to %d", sharedSamples[0])
	}
	if err := edit.Commit(); err != nil {
		t.Fatal(err)
	}
	if frame.Valid() {
		t.Fatal("committed copy did not consume the edited branch owner")
	}
	candidateSamples, _ := candidate.PlaneSamples(0)
	if candidateSamples[0] != 9 {
		t.Fatalf("candidate sample = %d", candidateSamples[0])
	}
	shared.Release()
	candidate.Release()
	if allocator.Used() != 0 {
		t.Fatalf("shared edit retained %d bytes", allocator.Used())
	}
}
