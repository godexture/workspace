package buffer

import (
	"math"
	"math/rand/v2"
	"testing"
)

// A layout comes from a component that may be wrong. Every specification must
// end in an error or a layout whose planes all fit the allocation; a layout
// that records more than it allocated is what turns a plugin bug into a Host
// panic and an under-charged allocator grant.
func TestLayoutRejectsOverflowingPlanes(t *testing.T) {
	cases := []struct {
		name  string
		spec  Spec
		valid bool
	}{
		// One unaligned plane of the maximum size is arithmetically consistent.
		// It is refused by the allocator grant, not by the layout.
		{"single max size", Spec{Planes: []PlaneSpec{{Size: math.MaxInt}}}, true},
		{"size and padding overflow", Spec{Planes: []PlaneSpec{{Size: math.MaxInt, Padding: math.MaxInt}}}, false},
		{"overflow wraps back to a small size", Spec{Planes: []PlaneSpec{
			{Size: 10},
			{Size: math.MaxInt, Padding: math.MaxInt},
		}}, false},
		{"accumulated planes overflow", Spec{Planes: []PlaneSpec{
			{Size: math.MaxInt / 2},
			{Size: math.MaxInt / 2},
			{Size: math.MaxInt / 2},
		}}, false},
		{"alignment padding overflows", Spec{Alignment: 1 << 20, Planes: []PlaneSpec{
			{Size: math.MaxInt - 8},
			{Size: 1},
		}}, false},
		{"negative size", Spec{Planes: []PlaneSpec{{Size: -1}}}, false},
		{"negative padding", Spec{Planes: []PlaneSpec{{Size: 1, Padding: -1}}}, false},
		{"non power of two alignment", Spec{Alignment: 3, Planes: []PlaneSpec{{Size: 1}}}, false},
		{"zero size plane", Spec{Planes: []PlaneSpec{{Size: 0}}}, true},
		{"no planes", Spec{}, true},
		{"padded planes", Spec{Alignment: 64, Planes: []PlaneSpec{{Size: 5, Padding: 3}, {Size: 7}}}, true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			layout, rawSize, err := layoutOf(testCase.spec)
			if !testCase.valid {
				if err == nil {
					t.Fatalf("layoutOf accepted %#v as %#v", testCase.spec, layout)
				}
				return
			}
			if err != nil {
				t.Fatalf("layoutOf rejected %#v: %v", testCase.spec, err)
			}
			assertLayoutFits(t, layout, rawSize)
		})
	}
}

// A layout that survives layoutOf must also survive allocation and every plane
// read, including the write paths that slice the backing storage directly.
func TestLayoutPropertyNeverPanics(t *testing.T) {
	random := rand.New(rand.NewPCG(1, 2))
	sizes := []int{0, 1, 7, 64, 4096, math.MaxInt / 4, math.MaxInt / 2, math.MaxInt - 1, math.MaxInt}
	alignments := []int{0, 1, 2, 16, 4096, 1 << 30}
	allocator := testAllocator(t, 1<<20)

	for iteration := 0; iteration < 500; iteration++ {
		spec := Spec{Alignment: alignments[random.IntN(len(alignments))]}
		for plane := random.IntN(4); plane >= 0; plane-- {
			spec.Planes = append(spec.Planes, PlaneSpec{
				Size:    sizes[random.IntN(len(sizes))],
				Padding: sizes[random.IntN(len(sizes))],
			})
		}
		layout, rawSize, err := layoutOf(spec)
		if err != nil {
			continue
		}
		assertLayoutFits(t, layout, rawSize)

		handle, err := allocator.Allocate(spec)
		if err != nil {
			continue
		}
		for index := range layout.Planes {
			if _, err := handle.Plane(index); err != nil {
				t.Fatalf("plane %d of %#v is unreadable: %v", index, layout, err)
			}
			if _, err := handle.PlaneAligned(index, 16); err != nil {
				t.Fatalf("plane %d of %#v has no alignment answer: %v", index, layout, err)
			}
		}
		edit, err := handle.Edit(allocator)
		if err != nil {
			handle.Release()
			continue
		}
		for index := range layout.Planes {
			if _, err := edit.MutablePlane(index); err != nil {
				t.Fatalf("mutable plane %d of %#v is unwritable: %v", index, layout, err)
			}
		}
		edit.Discard()
		handle.Release()
	}
}

func assertLayoutFits(t *testing.T, layout Layout, rawSize int) {
	t.Helper()
	if layout.Size < 0 || rawSize < 1 || rawSize < layout.Size {
		t.Fatalf("layout %#v has an inconsistent charge of %d", layout, rawSize)
	}
	for index, plane := range layout.Planes {
		if plane.Offset < 0 || plane.Size < 0 || plane.Padding < 0 {
			t.Fatalf("plane %d of %#v is negative", index, layout)
		}
		if plane.Offset%layout.Alignment != 0 {
			t.Fatalf("plane %d of %#v is not aligned", index, layout)
		}
		if plane.Offset+plane.Size+plane.Padding > layout.Size {
			t.Fatalf("plane %d of %#v exceeds the layout size", index, layout)
		}
	}
}
