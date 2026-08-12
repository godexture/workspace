package bind

import (
	"testing"

	"github.com/godexture/godec/access"
)

func TestSpoolCapabilityAdaptationIsOnlyASequentialToRandomWriteFallback(t *testing.T) {
	sequential, err := access.NewCapabilities(access.SequentialWrite)
	if err != nil {
		t.Fatal(err)
	}
	adapted, selected, ok := spoolCapabilities(sequential, access.NewRequirements(access.AllOf(access.RandomWrite)))
	if !ok || !adapted.Contains(access.SequentialWrite) || !adapted.Contains(access.RandomWrite) || len(selected.Capabilities()) != 1 || selected.Capabilities()[0] != access.RandomWrite {
		t.Fatalf("spool adaptation = %v, %v, %v", adapted.Values(), selected.Capabilities(), ok)
	}
	random, err := access.NewCapabilities(access.RandomWrite)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := spoolCapabilities(random, access.NewRequirements(access.AllOf(access.RandomWrite))); ok {
		t.Fatal("direct random-write capability was treated as a spool candidate")
	}
	if _, _, ok := spoolCapabilities(sequential, access.NewRequirements(access.AllOf(access.SequentialWrite))); ok {
		t.Fatal("already satisfied sequential requirement was treated as a spool candidate")
	}
}
