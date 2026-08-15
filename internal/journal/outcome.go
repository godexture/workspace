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

// Failure is one thing that went wrong, with where it went wrong. Node is
// snapshotted when the failure is recorded, because a task moves through the
// nodes of its island and the last one it was in does not describe the others.
// EventID is what makes two failures different things rather than two readings
// of one. Task is the number the group assigned, not the name: names are chosen
// for people and nothing keeps two tasks from sharing one, so a consumer that
// keyed on the name would fold two independent journals together.
//
// Operation is part of the identity for the same reason: a task's Run journal
// and the Flush journal performed afterward over the same slots both start
// counting from Seq 1, and both inherit the task's identity so a reader can
// tell they belong together. Without Operation here, their first failures
// would collide.
type EventID struct {
	Task      uint64
	Operation Operation
	Seq       uint64
}

// Task and Node say where the failure happened, for a reader. ID says which
// failure it is, for a consumer that must not report one twice.
type Failure struct {
	Kind  FailureKind
	ID    EventID
	Task  string
	Node  string
	Err   error
	Stack []byte
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
type Outcome struct {
	Operation Operation
	Task      string
	Primary   *Failure
	Cleanup   []Failure
}

func (o Outcome) Failed() bool { return o.Primary != nil || len(o.Cleanup) != 0 }

// The primary is what
// stopped the task; a release failure becomes the cause only when nothing else
// stopped it, and never replaces one that did.
// Cause is the single error a cancellation tree can carry.
// It is the error itself rather than the Failure around it: a cause travels
// through contexts that peers match against, and the attribution this journal
// adds is not part of what they are matching.
func (o Outcome) Cause() error {
	if o.Primary != nil {
		return o.Primary.Err
	}
	if len(o.Cleanup) != 0 {
		return o.Cleanup[0].Err
	}
	return nil
}

// Operation is the lifecycle step a journal covers. A task runs, then something
// flushes what it buffered, then whatever is left is discarded, and each is a
// separate attempt with its own single writer: the goroutine performing it.
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
