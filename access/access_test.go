package access

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/catalog"
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
	if !requirements.Valid() || len(requirements.Alternatives) != 2 || len(requirements.Alternatives[1].Capabilities) != 2 {
		t.Fatalf("requirements = %#v", requirements)
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
}

type providerComponentA struct{}
type providerComponentB struct{}
type providerPluginA struct{}
type providerPluginB struct{}
type providerConfig struct{}
type providerUnitID struct{}
type providerUnit int

type providerOperator struct{ shape flow.Shape }

func (o providerOperator) Ports() flow.Shape { return o.shape.Clone() }
func (providerOperator) Close() error        { return nil }

func providerSchema() config.Schema[providerConfig] {
	return config.Struct[providerConfig](func() providerConfig { return providerConfig{} }).Version("1").Build()
}

func providerFixtureComponent[Marker any](name string) plugin.Component {
	typ := schema.Define[providerUnitID, providerUnit](schema.Traits[providerUnit]{})
	shape := flow.NewShape(nil, []flow.Port{flow.Out("bytes", typ)})
	spec := plugin.Spec[providerConfig, flow.Shape, int]{
		Shape: plugin.StaticShape[providerConfig](shape),
		Compile: func(plugin.CompileContext, providerConfig, flow.Descriptors[int]) (plugin.Compiled[flow.Shape, int], error) {
			return plugin.Compiled[flow.Shape, int]{Plan: shape, Outputs: flow.NewDescriptors(flow.Describe("bytes", 1))}, nil
		},
		Open: func(_ plugin.OpenContext, plan flow.Shape) (flow.Operator, error) {
			return providerOperator{shape: plan}, nil
		},
	}
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: name}, providerSchema(), plugin.WithSpec(spec))
}

func TestProviderSchemeConflictUsesHostDeclarationValidation(t *testing.T) {
	first := providerFixtureComponent[providerComponentA]("first")
	second := providerFixtureComponent[providerComponentB]("second")
	firstProvider, err := DefineProvider[providerComponentA]([]string{"HTTP"})
	if err != nil {
		t.Fatal(err)
	}
	secondProvider, err := DefineProvider[providerComponentB]([]string{"http"})
	if err != nil {
		t.Fatal(err)
	}
	firstDeclaration, err := firstProvider.Declaration("http")
	if err != nil {
		t.Fatal(err)
	}
	secondDeclaration, err := secondProvider.Declaration("http")
	if err != nil {
		t.Fatal(err)
	}
	set := plugin.NewSet(
		plugin.Define[providerPluginA](plugin.Descriptor{DisplayName: "plugin-a"}, first),
		plugin.Define[providerPluginB](plugin.Descriptor{DisplayName: "plugin-b"}, second),
	).AddDeclaration(firstDeclaration).AddDeclaration(secondDeclaration)
	_, err = catalog.Build(set)
	if err == nil {
		t.Fatal("host accepted duplicate provider scheme")
	}
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "catalog.declaration-conflict" {
			return
		}
	}
	t.Fatalf("provider conflict diagnostic missing: %v", err)
}

func TestProviderManifestCarriesCapabilitiesRequirementsAndRole(t *testing.T) {
	capabilities, err := NewCapabilities(StableSize, RandomRead)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := DefineProvider[providerComponentA](
		[]string{"memory"},
		WithProviderRole(SourceSinkRole),
		WithProviderCapabilities(capabilities),
		WithProviderRequirements(NewRequirements(AnyOf(RandomRead, StableSize))),
		WithTransactionClass(AtomicReplace),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !provider.Role().AllowsSource() || !provider.Role().AllowsSink() || len(provider.Declarations()) != 2 {
		t.Fatalf("provider manifest = %#v", provider)
	}
	selection, ok := Select(provider.Capabilities(), provider.Requirements())
	if !ok || len(selection.Capabilities()) != 2 {
		t.Fatalf("provider selection = %v, %v", selection.Capabilities(), ok)
	}
}

func TestSpoolProbeAndSnapshotAreDeclarations(t *testing.T) {
	spool, err := NewSpoolSpec(1024, DiskSpool, 5*time.Millisecond, Rollbackable)
	if err != nil || spool.PredictedBytes() != 1024 || spool.RollbackClass() != Rollbackable {
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
