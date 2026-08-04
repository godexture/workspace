package access

import "errors"

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
