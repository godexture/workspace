package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/plugin"
)

type accessHandle struct{ closed *atomic.Int32 }

func (h accessHandle) Close() error {
	h.closed.Add(1)
	return nil
}

func TestOwnershipModesAreExplicit(t *testing.T) {
	closed := &atomic.Int32{}
	owned := Own(accessHandle{closed: closed})
	borrowed := Borrow(accessHandle{closed: closed})
	if owned.Ownership() != Owned || borrowed.Ownership() != Borrowed {
		t.Fatalf("ownership = %v, %v", owned.Ownership(), borrowed.Ownership())
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != 0 {
		t.Fatal("borrowed resource was closed")
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owned.Close(); err != nil || closed.Load() != 1 {
		t.Fatalf("owned close = %v, count = %d", err, closed.Load())
	}
}

func TestCopiedOwnedResourceClosesOnce(t *testing.T) {
	closed := &atomic.Int32{}
	first := Own(accessHandle{closed: closed})
	second := first
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != 1 {
		t.Fatalf("copied resource close count = %d, want 1", closed.Load())
	}
}

func TestFactoryCreatesOwnedSessionEachTime(t *testing.T) {
	var opens atomic.Int32
	factory := Factory(func(context.Context) (accessHandle, error) {
		opens.Add(1)
		return accessHandle{closed: &atomic.Int32{}}, nil
	})
	one, err := factory.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	two, err := factory.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if one.Ownership() != FactoryOwned || two.Ownership() != FactoryOwned || opens.Load() != 2 {
		t.Fatalf("factory sessions = %v, %v, opens=%d", one.Ownership(), two.Ownership(), opens.Load())
	}
}

func TestRequirementsAreCombinationsOfSmallCapabilities(t *testing.T) {
	requirements := NewRequirements(AnyOf(SequentialRead), AnyOf(RandomRead, StableSize))
	if !requirements.Valid() || !requirements.ValidFor(SourceDirection) || requirements.ValidFor(SinkDirection) || len(requirements.Alternatives) != 2 || len(requirements.Alternatives[1].Capabilities) != 2 {
		t.Fatalf("requirements = %#v", requirements)
	}
	write := NewRequirements(AnyOf(SequentialWrite), AnyOf(RandomWrite))
	if !write.ValidFor(SinkDirection) || write.ValidFor(SourceDirection) {
		t.Fatalf("write requirements = %#v", write)
	}
	if NewRequirements(AnyOf(SequentialRead, SequentialWrite)).ValidFor(SourceDirection) {
		t.Fatal("mixed-direction capability alternative was accepted")
	}
}

func TestCapabilitySelectionUsesDeclaredAlternativeOrderAndNarrows(t *testing.T) {
	available, err := NewCapabilities(StableSize, SequentialRead, RandomRead)
	if err != nil {
		t.Fatal(err)
	}
	requirements := NewRequirements(
		AnyOf(SequentialRead),
		AnyOf(RandomRead, StableSize),
	)
	selection, ok := Select(available, requirements)
	if !ok || !selection.Valid() {
		t.Fatal("available source did not satisfy its first alternative")
	}
	selected := selection.Capabilities()
	if len(selected) != 1 || selected[0] != SequentialRead {
		t.Fatalf("narrow selection = %v", selected)
	}
	selected[0] = RandomRead
	if selection.Capabilities()[0] != SequentialRead {
		t.Fatal("Selection exposed mutable capability storage")
	}

	insufficient, err := NewCapabilities(RandomRead)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Select(insufficient, requirements); ok {
		t.Fatal("incomplete random capability alternative was accepted")
	}
}

func TestReferenceSeparatesCanonicalAndRedactedDisplay(t *testing.T) {
	reference, err := Parse("https://user:password@example.com/file?X-Amz-Signature=secret#fragment-secret")
	if err != nil {
		t.Fatal(err)
	}
	if reference.Canonical() == reference.Display() || !strings.Contains(reference.Canonical(), "secret") {
		t.Fatalf("reference forms = %q, %q", reference.Canonical(), reference.Display())
	}
	if strings.Contains(reference.Display(), "password") || strings.Contains(reference.Display(), "X-Amz-Signature") || strings.Contains(reference.Display(), "fragment-secret") {
		t.Fatalf("redacted display leaked secret: %q", reference.Display())
	}
	if strings.Contains(fmt.Sprintf("%#v", reference), "password") {
		t.Fatalf("formatted reference leaked secret: %s", fmt.Sprintf("%#v", reference))
	}
	second, err := Parse("https://user:other@example.com/file?X-Amz-Signature=other")
	if err != nil {
		t.Fatal(err)
	}
	if reference.Fingerprint().IsZero() || reference.Fingerprint() == second.Fingerprint() {
		t.Fatal("private canonical references did not receive distinct nonzero fingerprints")
	}
}

type providerComponentA struct{}
type providerComponentB struct{}
type providerConfig struct{}
type providerUnitID struct{}
type providerUnit int

type providerOperator struct{ shape flow.Shape }
type providerSession struct{ capabilities Capabilities }

func (o providerOperator) Ports() flow.Shape { return o.shape.Clone() }
func (providerOperator) Close() error        { return nil }
func (s providerSession) Capabilities() Capabilities {
	result, _ := NewCapabilities(s.capabilities.Values()...)
	return result
}
func (providerSession) Close() error { return nil }

func providerSchema() config.Schema[providerConfig] {
	return config.Struct[providerConfig](func() providerConfig { return providerConfig{} }).Version("1").Build()
}

func providerFixtureComponent[Marker any](name string, shape flow.Shape, options ...plugin.ComponentOption) plugin.Component {
	spec := plugin.Spec[providerConfig, flow.Shape, int]{
		Shape: plugin.StaticShape[providerConfig](shape),
		Compile: func(plugin.CompileContext, providerConfig, flow.Descriptors[int]) (plugin.Compiled[flow.Shape, int], error) {
			outputs := flow.NewDescriptors[int]()
			for _, port := range shape.Outputs {
				outputs = flow.NewDescriptors(flow.Describe(port.ID(), 1))
			}
			return plugin.Compiled[flow.Shape, int]{Plan: shape, Outputs: outputs}, nil
		},
		Open: func(_ plugin.OpenContext, plan flow.Shape) (flow.Operator, error) {
			return providerOperator{shape: plan}, nil
		},
	}
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: name}, providerSchema(), append([]plugin.ComponentOption{plugin.WithSpec(spec)}, options...)...)
}

func TestAccessTraitsCarryCapabilitiesAcquireAndTransaction(t *testing.T) {
	capabilities, err := NewCapabilities(StableSize, RandomRead)
	if err != nil {
		t.Fatal(err)
	}
	acquire := func(context.Context, Reference, Selection) (Session, error) {
		return providerSession{capabilities: capabilities}, nil
	}
	typ := schema.Define[providerUnitID, providerUnit](schema.Traits[providerUnit]{})
	sourceComponent := providerFixtureComponent[providerComponentA](
		"source",
		flow.NewShape(nil, []flow.Port{flow.Out("bytes", typ)}),
		Source("MEMORY", capabilities, acquire),
	)
	sinkComponent := providerFixtureComponent[providerComponentB](
		"sink",
		flow.NewShape([]flow.Port{flow.In("bytes", typ)}, nil),
		Sink("memory", capabilities, AtomicReplace, acquire),
	)
	source, sourceOK := SourceOf(sourceComponent)
	sink, sinkOK := SinkOf(sinkComponent)
	if !sourceOK || !sinkOK || source.Scheme() != "memory" || sink.Scheme() != "memory" {
		t.Fatalf("traits = %#v/%v %#v/%v", source, sourceOK, sink, sinkOK)
	}
	if !source.Capabilities().Contains(RandomRead) || sink.TransactionClass() != AtomicReplace {
		t.Fatalf("trait capabilities = %#v %#v", source, sink)
	}
	reference, _ := Parse("memory:data")
	session, err := source.Acquire(context.Background(), reference, Selection{})
	if err != nil || !session.Capabilities().Contains(StableSize) {
		t.Fatalf("acquired session = %#v, %v", session, err)
	}
}

func TestSpoolProbeAndSnapshotAreDeclarations(t *testing.T) {
	spool, err := NewSpoolSpec(2048, 1024, DiskSpool, 5*time.Millisecond, true, Rollbackable)
	if err != nil || spool.MaximumBytes() != 2048 || spool.PredictedBytes() != 1024 || !spool.FinalCopy() || spool.RollbackClass() != Rollbackable {
		t.Fatalf("spool = %#v, %v", spool, err)
	}
	view := NewProbeView([]byte("probe"))
	buffer := make([]byte, 3)
	if n, err := view.ReadAt(context.Background(), buffer, 1); err != nil || n != 3 || string(buffer) != "rob" {
		t.Fatalf("probe read = %d, %q, %v", n, buffer, err)
	}
	request, err := NewRangeRequest(10, 4)
	if err != nil || request.Offset() != 10 || request.Length() != 4 {
		t.Fatalf("probe range request = %#v, %v", request, err)
	}
	snapshot, err := NewSnapshot("", NoSnapshot)
	if err != nil || !snapshot.Valid() || snapshot.Strong() {
		t.Fatalf("snapshot = %#v, %v", snapshot, err)
	}
}

func TestSpoolSpecRejectsMissingQuotaAndPredictionAboveQuota(t *testing.T) {
	if _, err := NewSpoolSpec(0, 0, MemorySpool, 0, true, AtomicReplace); !errors.Is(err, ErrInvalidSpoolSpec) {
		t.Fatalf("missing quota error = %v", err)
	}
	if _, err := NewSpoolSpec(4, 5, MemorySpool, 0, true, AtomicReplace); !errors.Is(err, ErrInvalidSpoolSpec) {
		t.Fatalf("prediction above quota error = %v", err)
	}
}
