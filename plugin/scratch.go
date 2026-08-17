package plugin

import "context"

// Scratch is the narrow borrowed journal granted to one component instance.
// Append atomically writes one full record and returns its starting offset only
// on success; positioned reads and writes must stay entirely within bytes
// already appended.
type Scratch interface {
	Append(context.Context, []byte) (int64, error)
	ReadAt(context.Context, []byte, int64) error
	WriteAt(context.Context, []byte, int64) error
}
