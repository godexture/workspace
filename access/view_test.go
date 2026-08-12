package access

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type capabilityViewSession struct {
	capabilities Capabilities
}

func (s capabilityViewSession) Capabilities() Capabilities { return cloneCapabilities(s.capabilities) }
func (capabilityViewSession) Close() error                 { return nil }
func (capabilityViewSession) Read(context.Context, []byte) (int, error) {
	return 0, nil
}
func (capabilityViewSession) ReadAt(context.Context, []byte, int64) (int, error) {
	return 0, nil
}
func (capabilityViewSession) Size(context.Context) (int64, error) { return 42, nil }
func (capabilityViewSession) Write(context.Context, []byte) (int, error) {
	return 0, nil
}
func (capabilityViewSession) WriteAt(context.Context, []byte, int64) (int, error) {
	return 0, nil
}

type capabilityOnlySession struct {
	capabilities Capabilities
}

func (s capabilityOnlySession) Capabilities() Capabilities { return cloneCapabilities(s.capabilities) }
func (capabilityOnlySession) Close() error                 { return nil }

type randomOnlySession struct{ capabilityOnlySession }

func (randomOnlySession) ReadAt(context.Context, []byte, int64) (int, error) { return 0, nil }

func TestViewsForNarrowsSelectedOperations(t *testing.T) {
	available, err := NewCapabilities(SequentialRead, RandomRead, StableSize, SequentialWrite, RandomWrite)
	if err != nil {
		t.Fatal(err)
	}
	selection, ok := Select(available, NewRequirements(AllOf(SequentialRead, RandomWrite, StableSize)))
	if !ok {
		t.Fatal("capability selection failed")
	}
	views, err := viewsFor(capabilityViewSession{capabilities: available}, selection)
	if err != nil {
		t.Fatal(err)
	}
	if views.sequential == nil || views.sizer == nil || views.patcher == nil || views.random != nil || views.appender != nil {
		t.Fatalf("narrow views = %#v", views)
	}
}

func TestViewsForRejectsStableSizeWithoutSizer(t *testing.T) {
	available, err := NewCapabilities(RandomRead, StableSize)
	if err != nil {
		t.Fatal(err)
	}
	selection, ok := Select(available, NewRequirements(AllOf(RandomRead, StableSize)))
	if !ok {
		t.Fatal("capability selection failed")
	}
	session := randomOnlySession{capabilityOnlySession{capabilities: available}}
	if _, err := viewsFor(session, selection); !errors.Is(err, ErrCapabilityView) || !strings.Contains(err.Error(), string(StableSize)) {
		t.Fatalf("missing stable size view error = %v", err)
	}
}

func TestViewsForRejectsMissingSelectedOperation(t *testing.T) {
	available, err := NewCapabilities(SequentialWrite)
	if err != nil {
		t.Fatal(err)
	}
	selection, ok := Select(available, NewRequirements(AllOf(SequentialWrite)))
	if !ok {
		t.Fatal("capability selection failed")
	}
	if _, err := viewsFor(capabilityOnlySession{capabilities: available}, selection); !errors.Is(err, ErrCapabilityView) {
		t.Fatalf("missing Appender error = %v", err)
	}
}
