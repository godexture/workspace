package access

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/host"
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
	if len(requirements.Alternatives) != 2 || len(requirements.Alternatives[1].Capabilities) != 2 {
		t.Fatalf("requirements = %#v", requirements)
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

func providerSchema() config.Schema[providerConfig] {
	return config.Struct[providerConfig](func() providerConfig { return providerConfig{} }).Version("1").Build()
}

func TestProviderSchemeConflictUsesHostDeclarationValidation(t *testing.T) {
	first := plugin.NewComponent[providerComponentA](plugin.Descriptor{DisplayName: "first"}, providerSchema())
	second := plugin.NewComponent[providerComponentB](plugin.Descriptor{DisplayName: "second"}, providerSchema())
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
	if _, err := host.New(host.Plugins(set)); err == nil {
		t.Fatal("host accepted duplicate provider scheme")
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
