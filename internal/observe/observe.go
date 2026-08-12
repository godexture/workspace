// Package observe collects island-local measurements into bounded immutable
// job snapshots and optional live delivery. Off mode creates no collector,
// local counter, queue, dispatcher, or clock read.
package observe

import (
	"context"
	"time"
)

type Mode uint8

const (
	Off Mode = iota
	Basic
	Detailed
	Trace
)

func (m Mode) Valid() bool { return m <= Trace }

type Kind uint8

const (
	Progress Kind = iota + 1
	Lifecycle
	Diagnostic
)

func (k Kind) Valid() bool { return k >= Progress && k <= Diagnostic }

// Event is the single internal observation record. Zero At and byte/time
// fields are intentional at lower observation levels.
type Event struct {
	Sequence uint64
	Kind     Kind
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

func (e Event) clone() Event {
	e.Detail = cloneMap(e.Detail)
	return e
}

type Clock func() time.Time
type Sink func(context.Context, Event) error

type Config struct {
	HistoryLimit  int
	DeliveryLimit int
	Sink          Sink
	Context       context.Context
	Fail          func(error)
}

type Summary struct {
	HistoryDropped  uint64
	DeliveryDropped uint64
}

func cloneMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
