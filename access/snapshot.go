package access

import (
	"context"
	"errors"
)

// SnapshotNature records whether a source can identify a stable view.
type SnapshotNature uint8

const (
	NoSnapshot SnapshotNature = iota + 1
	WeakSnapshot
	StrongSnapshot
)

func (n SnapshotNature) Valid() bool { return n >= NoSnapshot && n <= StrongSnapshot }

var ErrInvalidSnapshot = errors.New("access snapshot identity is invalid")

// Snapshot is a source observation. NoSnapshot is a valid explicit value and
// records that the source cannot provide a strong snapshot identity.
type Snapshot struct {
	identity string
	nature   SnapshotNature
}

func NewSnapshot(identity string, nature SnapshotNature) (Snapshot, error) {
	if !nature.Valid() || (nature == StrongSnapshot && identity == "") || (nature == NoSnapshot && identity != "") {
		return Snapshot{}, ErrInvalidSnapshot
	}
	return Snapshot{identity: identity, nature: nature}, nil
}

func (s Snapshot) Valid() bool {
	return s.nature.Valid() && !(s.nature == StrongSnapshot && s.identity == "") && !(s.nature == NoSnapshot && s.identity != "")
}
func (s Snapshot) Identity() string       { return s.identity }
func (s Snapshot) Nature() SnapshotNature { return s.nature }
func (s Snapshot) Strong() bool           { return s.nature == StrongSnapshot && s.identity != "" }

// Snapshotter reports the content identity a source session is currently
// serving. A session is expected to implement it whenever it promises
// StableSize: probe, inspect, and run all read through one session, and Host
// compares the identity across those phases so planning facts and executed
// bytes cannot silently describe different content.
//
// The session reports; it does not judge. Deciding that a changed identity
// ends the job belongs to Host, which is the only party that knows which
// phases have already run.
type Snapshotter interface {
	Snapshot(context.Context) (Snapshot, error)
}

// SnapshotOf reports the snapshot view of a session, if it has one.
func SnapshotOf(session Session) (Snapshotter, bool) {
	value, ok := session.(Snapshotter)
	return value, ok && value != nil
}
