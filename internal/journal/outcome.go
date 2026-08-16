package journal

import "fmt"

// FailureKind separates what stopped the work from what could not be released
// afterwards, and an error a stage returned from a panic it did not survive.
type FailureKind uint8

const (
	TaskError FailureKind = iota + 1
	TaskPanic
	CleanupError
	CleanupPanic
)

func (k FailureKind) String() string {
	switch k {
	case TaskError:
		return "error"
	case TaskPanic:
		return "panic"
	case CleanupError:
		return "cleanup error"
	case CleanupPanic:
		return "cleanup panic"
	default:
		return "unknown"
	}
}

func (k FailureKind) Cleanup() bool { return k == CleanupError || k == CleanupPanic }

// EventID is what makes two failures different things rather than two readings
// of one. Task is the number the group assigned, not the name: names are
// chosen for people and nothing keeps two tasks from sharing one, so a
// consumer that keyed on the name would fold two independent journals
// together.
//
// Attempt tells apart two Scope objects opened for the same Task -- a task's
// Run journal and the Flush journal namedTask.flush opens afterward both start
// counting Seq from one, so without Attempt their first failures would
// collide. It carries no meaning beyond that: it says nothing about which
// lifecycle operation a failure belongs to, so relabeling a journal in place
// (Scope.EnterOperation) never has to touch it, and a Scope that is never
// reopened can relabel as many times as it likes without risking a collision.
type EventID struct {
	Task    uint64
	Attempt uint64
	Seq     uint64
}

// Task and Node say where the failure happened, for a reader. ID says which
// failure it is, for a consumer that must not report one twice. Operation says
// which lifecycle step it belongs to, for a consumer mapping it to its own
// vocabulary (Host's Phase); it is metadata about the failure, not part of
// what makes the failure unique.
type Failure struct {
	Kind      FailureKind
	ID        EventID
	Operation Operation
	Task      string
	Node      string
	Err       error
	Stack     []byte
}

func (f Failure) Error() string {
	if f.Node == "" {
		return fmt.Sprintf("task %q %s: %v", f.Task, f.Kind, f.Err)
	}
	return fmt.Sprintf("task %q %s at %s: %v", f.Task, f.Kind, f.Node, f.Err)
}

func (f Failure) Unwrap() error { return f.Err }

// Outcome is what one task ended with: the failure that stopped it, and the
// releases it could not perform. They stay separate all the way to the caller,
// because a release that failed while the task was already stopping never
// explains why it stopped.
//
// It carries no Operation of its own: the goroutine that owns a Scope can
// relabel it mid-lifetime (EnterOperation), so two failures in the same
// Outcome can belong to different operations. Each Failure's own Operation
// says which.
type Outcome struct {
	Task    string
	Primary *Failure
	Cleanup []Failure
}

func (o Outcome) Failed() bool { return o.Primary != nil || len(o.Cleanup) != 0 }

// Cause is the single error a cancellation tree can carry, tagged with the
// event that produced it. A peer that only observes the cancellation and
// propagates context.Cause verbatim carries this exact value onward, so a
// later consumer can recognize a second sighting of the one event that
// stopped the run by identity -- comparing Event -- rather than guessing from
// what the error says, which two unrelated failures could say identically by
// coincidence.
type Cause struct {
	Event EventID
	Err   error
}

func (c *Cause) Error() string { return c.Err.Error() }
func (c *Cause) Unwrap() error { return c.Err }

// Cause returns this outcome's Cause: the primary is what stopped the task; a
// release failure becomes the cause only when nothing else stopped it, and
// never replaces one that did.
func (o Outcome) Cause() error {
	if o.Primary != nil {
		return &Cause{Event: o.Primary.ID, Err: o.Primary.Err}
	}
	if len(o.Cleanup) != 0 {
		return &Cause{Event: o.Cleanup[0].ID, Err: o.Cleanup[0].Err}
	}
	return nil
}

// Operation is the lifecycle step a failure belongs to. A task runs, then
// something flushes what it buffered, then whatever is left is discarded.
type Operation uint8

const (
	Run Operation = iota + 1
	Flush
	Discard
)

func (o Operation) String() string {
	switch o {
	case Run:
		return "run"
	case Flush:
		return "flush"
	case Discard:
		return "discard"
	default:
		return "unknown"
	}
}
