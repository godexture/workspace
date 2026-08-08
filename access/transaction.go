package access

import (
	"context"
	"errors"
)

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

// Flusher publishes buffered bytes to the opened sink session. It does not
// make an output externally visible.
type Flusher interface {
	Flush(context.Context) error
}

// Syncer asks a durable sink to persist previously flushed bytes.
type Syncer interface {
	Sync(context.Context) error
}

// Transaction is coordinated by Host after every component has finalized and
// every sink has flushed and synced. Abort must be safe before Commit and may
// be attempted after an indeterminate Commit failure.
type Transaction interface {
	PrepareCommit(context.Context) error
	Commit(context.Context) error
	Abort(context.Context) error
}
