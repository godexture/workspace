package access_test

import (
	"context"
	"fmt"

	"github.com/godexture/godec/access"
)

type accessExampleCloser struct{ closed bool }

func (c *accessExampleCloser) Close() error {
	c.closed = true
	return nil
}

// Requirements express alternatives as comparable capability data that can
// be recorded in a plan, while a probe receives only a bounded Random view.
func ExampleNewRequirements() {
	requirements := access.NewRequirements(
		access.AnyOf(access.SequentialRead),
		access.AnyOf(access.RandomRead, access.StableSize),
	)
	probe, err := access.NewProbeViewAt(128, []byte("fLaC"))
	if err != nil {
		panic(err)
	}
	buffer := make([]byte, 4)
	_, err = probe.ReadAt(context.Background(), buffer, 128)
	if err != nil {
		panic(err)
	}

	fmt.Println(len(requirements.Alternatives), requirements.Alternatives[1].Capabilities)
	fmt.Println(probe.Range().Offset(), string(buffer))
	// Output:
	// 2 [random-read stable-size]
	// 128 fLaC
}

// Select fixes one declared alternative before Open, so a later Format sees
// only the operations it requested even when the source supports more.
func ExampleSelect() {
	available, _ := access.NewCapabilities(access.SequentialRead, access.RandomRead, access.StableSize)
	requirements := access.NewRequirements(
		access.AnyOf(access.SequentialRead),
		access.AnyOf(access.RandomRead, access.StableSize),
	)
	selection, ok := access.Select(available, requirements)

	fmt.Println(ok, selection.Capabilities())
	// Output: true [sequential-read]
}

// A strong snapshot gives repeated planning and execution a stable source
// identity; absence of snapshot support is represented explicitly.
func ExampleNewSnapshot() {
	stable, _ := access.NewSnapshot("etag:abc123", access.StrongSnapshot)
	unsupported, _ := access.NewSnapshot("", access.NoSnapshot)

	fmt.Println(stable.Strong(), stable.Identity())
	fmt.Println(unsupported.Valid(), unsupported.Strong())
	// Output:
	// true etag:abc123
	// true false
}

// A reference keeps its resolver-facing form while redacting credentials and
// parameters from every ordinary string representation.
func ExampleParse() {
	reference, err := access.Parse("https://user:secret@example.com/audio.wav?token=secret#part")
	if err != nil {
		panic(err)
	}

	fmt.Println(reference.Scheme())
	fmt.Println(reference.Display())
	fmt.Println(reference.Canonical() != reference.Display())
	// Output:
	// https
	// https://example.com/audio.wav?redacted#redacted
	// true
}

// Own transfers close responsibility to the resource; Borrow leaves it with
// the caller.
func ExampleOwn() {
	ownedValue := &accessExampleCloser{}
	borrowedValue := &accessExampleCloser{}
	owned := access.Own(ownedValue)
	borrowed := access.Borrow(borrowedValue)

	_ = owned.Close()
	_ = borrowed.Close()
	fmt.Println(ownedValue.closed, borrowedValue.closed)
	// Output: true false
}
