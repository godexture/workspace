package host

import (
	"context"
	"fmt"
	"time"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
)

// Observation controls work performed by the runtime data path.
type Observation uint8

const (
	ObservationOff Observation = iota
	ObservationBasic
	ObservationDetailed
	ObservationTrace
)

func (o Observation) Valid() bool { return o <= ObservationTrace }

// Phase identifies the lifecycle boundary at which a failure occurred.
type Phase string

const (
	PreparePhase       Phase = "prepare"
	OpenPhase          Phase = "open"
	RunPhase           Phase = "run"
	ObservationPhase   Phase = "observation"
	FinalizePhase      Phase = "finalize"
	FlushPhase         Phase = "flush"
	SyncPhase          Phase = "sync"
	PrepareCommitPhase Phase = "prepare-commit"
	CommitPhase        Phase = "commit"
	AbortPhase         Phase = "abort"
	ClosePhase         Phase = "close"
	JoinPhase          Phase = "join"
	DiscardPhase       Phase = "discard"
	ResourcePhase      Phase = "resource"
	// UnknownPhase is used only when bounded aggregation has intentionally
	// discarded the operation metadata of its coarse overflow group.
	UnknownPhase Phase = "unknown"
)

// EventID identifies one failure within one Run.
//
// It is what makes two failures different things rather than two readings of
// one. A consumer that must not report a failure twice compares this, never
// what two errors say: two components failing the same way are two failures,
// and one failure seen at four boundaries is one. It is zero for a failure
// produced outside a Run, by Prepare or by Close.
type EventID struct {
	Run uint64
	Seq uint64
}

func (e EventID) Valid() bool { return e.Run != 0 && e.Seq != 0 }

// Failure retains the original error, its stable execution location, and the
// identity of the event it was recorded as.
type Failure struct {
	ID    EventID
	Phase Phase
	Node  string
	Task  string
	Err   error
	Stack []byte
}

func (f Failure) Error() string {
	location := f.Node
	if f.Task != "" {
		location = f.Task
	}
	if location == "" {
		return fmt.Sprintf("%s: %v", f.Phase, f.Err)
	}
	return fmt.Sprintf("%s %s: %v", f.Phase, location, f.Err)
}

func (f Failure) Unwrap() error { return f.Err }

type OutputState uint8

const (
	OutputPending OutputState = iota + 1
	OutputCommitted
	OutputAborted
	OutputUnknown
)

func (s OutputState) String() string {
	switch s {
	case OutputPending:
		return "pending"
	case OutputCommitted:
		return "committed"
	case OutputAborted:
		return "aborted"
	case OutputUnknown:
		return "unknown"
	default:
		return "invalid"
	}
}

// OutputOutcome reports each sink independently because multi-output commit
// cannot generally be atomic across providers.
type OutputOutcome struct {
	Node              string
	Component         string
	Class             access.TransactionClass
	State             OutputState
	RollbackAttempted bool
}

type EventKind uint8

const (
	ProgressEvent EventKind = iota + 1
	LifecycleEvent
	DiagnosticEvent
)

// Event is an immutable observation snapshot. Detail is copied before Run
// returns and is never shared with a component.
type Event struct {
	Sequence uint64
	Kind     EventKind
	Node     string
	Edge     string
	Phase    string
	Code     string
	Message  string
	Items    uint64
	Bytes    uint64
	Media    int64
	HasMedia bool
	At       time.Time
	Detail   map[string]string
}

// EventSink receives immutable event snapshots from one Run. Calls are
// serialized by Host and never execute on a media-path goroutine.
type EventSink interface {
	Emit(context.Context, Event) error
}

// EventSinkFunc adapts a function to EventSink.
type EventSinkFunc func(context.Context, Event) error

func (f EventSinkFunc) Emit(ctx context.Context, event Event) error {
	if f == nil {
		return nil
	}
	return f(ctx, event)
}

// ObservationSummary distinguishes bounded history loss from live delivery
// overflow. Event sequence gaps identify where delivery was lost.
type ObservationSummary struct {
	HistoryDropped  uint64
	DeliveryDropped uint64
}

// Suppressed reports failures a run counted but did not keep in full.
//
// A run's evidence is bounded, so a component that fails to release a hundred
// thousand payloads produces a hundred thousand counted occurrences and a
// couple of retained samples rather than a hundred thousand stacks. What is
// never dropped is that they happened, how many there were, which class they
// belonged to, and that the rest were omitted. An empty Suppressed means every
// failure appears in full.
type Suppressed struct {
	// Phase is UnknownPhase for the global overflow group, whose budget bound
	// intentionally discarded operation metadata.
	Phase Phase
	Node  string
	Task  string
	// Class is the failure class the run grouped by: a diagnostic code, or the
	// error's Go type. It is never the error's message, which can carry a
	// payload and can vary until every occurrence is its own class.
	Class string
	// Kind separates a release that failed from one that panicked.
	Kind string
	// Occurrences is every occurrence of this class, including the ones kept in
	// full. It saturates rather than wrapping.
	Occurrences uint64
	// Retained is how many representative samples the ledger retained. A
	// stopping provenance record can appear separately in Primary or Cleanup.
	Retained uint64
	// First and Last identify the earliest and latest occurrence.
	First EventID
	Last  EventID
	// Truncated reports that detail was dropped by the run's budget rather than
	// never having existed.
	Truncated bool
	// Classes counts how many distinct classes were folded into this entry. It
	// is 1 normally, and larger when the run saw more classes than it tracks
	// separately and fell back to a coarser grouping.
	Classes uint64
	// ClassesTruncated says Classes is a lower bound rather than an exact total.
	ClassesTruncated bool
}

func (s Suppressed) Error() string {
	if s.ClassesTruncated {
		return fmt.Sprintf("%s %s: %d occurrences across at least %d classes, %d representative samples (detail truncated)",
			s.Phase, s.location(), s.Occurrences, s.Classes, s.Retained)
	}
	if s.Classes > 1 {
		return fmt.Sprintf("%s %s: %d occurrences across %d classes, %d representative samples (detail truncated)",
			s.Phase, s.location(), s.Occurrences, s.Classes, s.Retained)
	}
	return fmt.Sprintf("%s %s: %s occurred %d times, %d representative samples",
		s.Phase, s.location(), s.Class, s.Occurrences, s.Retained)
}

func (s Suppressed) location() string {
	if s.Task != "" {
		return s.Task
	}
	if s.Node != "" {
		return s.Node
	}
	return "run"
}

// Omitted is how many occurrences were counted without a representative
// sample. Primary/Cleanup may separately contain the one stopping provenance.
func (s Suppressed) Omitted() uint64 { return s.Occurrences - s.Retained }

// Result separates three different things a run can produce, because
// collapsing any two of them loses something a caller needs.
//
// Primary is the failure that stopped the run: the earliest one that stopped
// useful work, and so the one every later failure is downstream of.
//
// Secondary is every other independent failure of useful work. Two components
// can fail at the same time without either being the other's consequence.
// Reporting only one of them, calling the rest cleanup, or leaving them in
// diagnostics would each describe that run as something it was not. A failure
// that is merely the same event observed again is not here and is not
// anywhere: it is not a second failure, and the run recognizes it by identity
// rather than by what it says.
//
// Cleanup is everything that could not be released, closed, rolled back, or
// discarded. It never explains why the run stopped, because it happened while
// the run was already stopping.
type Result struct {
	Primary   *Failure
	Secondary []Failure
	Cleanup   []Failure
	// Suppressed accounts for repetition the run counted rather than copied. It
	// is empty unless a failure class occurred more times than the run keeps in
	// full.
	Suppressed  []Suppressed
	Diagnostics []diagnostic.Item
	Outputs     []OutputOutcome
	Events      []Event
	Observation ObservationSummary
}

func (r Result) Succeeded() bool {
	if r.Primary != nil || len(r.Secondary) != 0 || len(r.Cleanup) != 0 || len(r.Suppressed) != 0 {
		return false
	}
	for _, output := range r.Outputs {
		if output.State != OutputCommitted {
			return false
		}
	}
	return true
}
