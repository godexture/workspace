package host

import (
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
	FinalizePhase      Phase = "finalize"
	FlushPhase         Phase = "flush"
	SyncPhase          Phase = "sync"
	PrepareCommitPhase Phase = "prepare-commit"
	CommitPhase        Phase = "commit"
	AbortPhase         Phase = "abort"
	ClosePhase         Phase = "close"
	JoinPhase          Phase = "join"
	ResourcePhase      Phase = "resource"
)

// Failure retains the original error and its stable execution location.
type Failure struct {
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

// Result separates the failure that stopped useful work from failures found
// while rolling back or releasing resources.
type Result struct {
	Primary     *Failure
	Cleanup     []Failure
	Diagnostics []diagnostic.Item
	Outputs     []OutputOutcome
	Events      []Event
}

func (r Result) Succeeded() bool {
	if r.Primary != nil || len(r.Cleanup) != 0 {
		return false
	}
	for _, output := range r.Outputs {
		if output.State != OutputCommitted {
			return false
		}
	}
	return true
}
