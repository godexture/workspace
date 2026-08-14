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
type Failure struct {
	Kind  FailureKind
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
	Task    string
	Primary *Failure
	Cleanup []Failure
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
