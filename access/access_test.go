package access

import (
	"context"
	"sync/atomic"
	"testing"
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
