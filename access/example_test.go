package access_test

import (
	"context"
	"fmt"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/plugin"
)

type accessExampleCloser struct{ closed bool }
type accessTraitComponentID struct{}
type accessTraitConfig struct{}
type accessTraitSchemaID struct{}
type accessTraitUnit int

type accessExampleIO struct {
	read    []byte
	written []byte
}

type accessTraitOperator struct{ shape flow.Shape }

func (o accessTraitOperator) Ports() flow.Shape { return o.shape.Clone() }
func (accessTraitOperator) Close() error        { return nil }

type accessTraitSession struct{ capabilities access.Capabilities }

func (s accessTraitSession) Capabilities() access.Capabilities { return s.capabilities }
func (accessTraitSession) Close() error                        { return nil }

func (c *accessExampleCloser) Close() error {
	c.closed = true
	return nil
}

func (s *accessExampleIO) Read(context.Context, []byte) (int, error) { return 0, nil }
func (s *accessExampleIO) ReadAt(_ context.Context, destination []byte, offset int64) (int, error) {
	return copy(destination, s.read[offset:]), nil
}
func (s *accessExampleIO) Write(_ context.Context, source []byte) (int, error) {
	s.written = append(s.written, source...)
	return len(source), nil
}
func (s *accessExampleIO) WriteAt(_ context.Context, source []byte, offset int64) (int, error) {
	copy(s.written[offset:], source)
	return len(source), nil
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

// Read and write views are context-aware and keep cursor-based operations
// separate from position-independent operations.
func ExampleAppender() {
	stream := &accessExampleIO{read: []byte("read")}
	var random access.Random = stream
	var appender access.Appender = stream
	var patcher access.Patcher = stream

	read := make([]byte, 4)
	_, _ = random.ReadAt(context.Background(), read, 0)
	_, _ = appender.Write(context.Background(), []byte("write"))
	_, _ = patcher.WriteAt(context.Background(), []byte("W"), 0)

	fmt.Println(string(read), string(stream.written))
	// Output: read Write
}

// Access behavior and typed execution are traits of the same component, so a
// plugin definition is the only value an application needs to compose.
func ExampleSource() {
	typ := schema.Define[accessTraitSchemaID, accessTraitUnit](schema.Traits[accessTraitUnit]{})
	shape := flow.NewShape(nil, []flow.Port{flow.Out("bytes", typ)})
	capabilities, _ := access.NewCapabilities(access.SequentialRead)
	acquire := func(context.Context, access.Reference, access.Selection) (access.Session, error) {
		return accessTraitSession{capabilities: capabilities}, nil
	}
	component := plugin.NewComponent[accessTraitComponentID](
		plugin.Descriptor{DisplayName: "memory source", Version: "1"},
		config.Struct[accessTraitConfig](func() accessTraitConfig { return accessTraitConfig{} }).Version("1").Build(),
		plugin.WithSpec(plugin.Spec[accessTraitConfig, flow.Shape, int]{
			Shape: plugin.StaticShape[accessTraitConfig](shape),
			Compile: func(plugin.CompileContext, accessTraitConfig, flow.Descriptors[int]) (plugin.Compiled[flow.Shape, int], error) {
				return plugin.Compiled[flow.Shape, int]{Plan: shape, Outputs: flow.NewDescriptors(flow.Describe("bytes", 1))}, nil
			},
			Open: func(plugin.OpenContext, flow.Shape) (flow.Operator, error) {
				return accessTraitOperator{shape: shape}, nil
			},
		}),
		plugin.WithReader("bytes", typ),
		access.Source("memory", capabilities, acquire),
	)
	trait, ok := access.SourceOf(component)

	fmt.Println(ok, trait.Scheme())
	fmt.Println(component.View().Executable, len(component.Traits()))
	// Output:
	// true memory
	// true 1
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
