package access

import (
	"context"
	"errors"
	"testing"
)

type openingSession struct {
	capabilities Capabilities
}

func (s openingSession) Capabilities() Capabilities { return s.capabilities }
func (openingSession) Close() error                 { return nil }
func (openingSession) Read(context.Context, []byte) (int, error) {
	return 0, nil
}
func (openingSession) Write(context.Context, []byte) (int, error) {
	return 0, nil
}
func (openingSession) PrepareCommit(context.Context) error { return nil }
func (openingSession) Commit(context.Context) error        { return nil }
func (openingSession) Abort(context.Context) error         { return nil }
func (openingSession) Flush(context.Context) error         { return nil }
func (openingSession) Sync(context.Context) error          { return nil }

func TestOpeningExposesOnlySelectedNarrowViews(t *testing.T) {
	capabilities, err := NewCapabilities(SequentialRead, SequentialWrite)
	if err != nil {
		t.Fatal(err)
	}
	session := openingSession{capabilities: capabilities}
	readSelection, ok := Select(capabilities, NewRequirements(AnyOf(SequentialRead)))
	if !ok {
		t.Fatal("read selection failed")
	}
	read, err := NewOpening(SourceDirection, session, readSelection, 0)
	if err != nil || !read.Valid() {
		t.Fatalf("read opening = %#v, %v", read, err)
	}
	if _, ok := SequentialOf(read); !ok {
		t.Fatal("selected Sequential view is missing")
	}
	if _, ok := AppenderOf(read); ok {
		t.Fatal("unselected Appender view was exposed")
	}

	writeSelection, ok := Select(capabilities, NewRequirements(AnyOf(SequentialWrite)))
	if !ok {
		t.Fatal("write selection failed")
	}
	write, err := NewOpening(SinkDirection, session, writeSelection, AtomicReplace)
	if err != nil || !write.Valid() {
		t.Fatalf("write opening = %#v, %v", write, err)
	}
	if _, ok := AppenderOf(write); !ok {
		t.Fatal("selected Appender view is missing")
	}
	if _, ok := TransactionOf(write); !ok {
		t.Fatal("sink transaction is missing")
	}
	if _, ok := FlusherOf(write); !ok {
		t.Fatal("sink flusher is missing")
	}
	if _, ok := SyncerOf(write); !ok {
		t.Fatal("sink syncer is missing")
	}
}

func TestOpeningRejectsMissingOperationAndTransactionViews(t *testing.T) {
	readCapabilities, _ := NewCapabilities(SequentialRead)
	readSelection, _ := Select(readCapabilities, NewRequirements(AnyOf(SequentialRead)))
	if _, err := NewOpening(SourceDirection, capabilityOnlySession{capabilities: readCapabilities}, readSelection, 0); !errors.Is(err, ErrCapabilityView) {
		t.Fatalf("missing read view error = %v", err)
	}
	writeCapabilities, _ := NewCapabilities(SequentialWrite)
	writeSelection, _ := Select(writeCapabilities, NewRequirements(AnyOf(SequentialWrite)))
	type appendSession struct{ capabilityViewSession }
	session := appendSession{capabilityViewSession{capabilities: writeCapabilities}}
	if _, err := NewOpening(SinkDirection, session, writeSelection, AtomicReplace); !errors.Is(err, ErrTransactionView) {
		t.Fatalf("missing transaction error = %v", err)
	}
}
