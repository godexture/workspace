package access

import "errors"

// TransactionClass describes the sink commit contract without defining its
// execution lifecycle. Execution and rollback belong to M5.
type TransactionClass uint8

const (
	AtomicReplace TransactionClass = iota + 1
	StagedCommit
	Rollbackable
	AppendOnly
	LiveNoCommit
)

func (c TransactionClass) Valid() bool { return c >= AtomicReplace && c <= LiveNoCommit }

func (c TransactionClass) String() string {
	switch c {
	case AtomicReplace:
		return "atomic-replace"
	case StagedCommit:
		return "staged-commit"
	case Rollbackable:
		return "rollbackable"
	case AppendOnly:
		return "append-only"
	case LiveNoCommit:
		return "live-no-commit"
	default:
		return "unknown"
	}
}

var ErrInvalidTransactionClass = errors.New("access transaction class is invalid")
