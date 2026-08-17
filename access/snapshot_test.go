package access

import "testing"

// A snapshot exists to be compared. A weak snapshot with no identity compares
// equal to every other one, so it would let any content pass a check that is
// supposed to notice a change; only NoSnapshot may say nothing.
func TestSnapshotRequiresAnIdentityUnlessItDeclaresNone(t *testing.T) {
	cases := []struct {
		name     string
		identity string
		nature   SnapshotNature
		valid    bool
	}{
		{name: "strong with identity", identity: "etag:1", nature: StrongSnapshot, valid: true},
		{name: "weak with identity", identity: "size:1/mtime:2", nature: WeakSnapshot, valid: true},
		{name: "none without identity", nature: NoSnapshot, valid: true},
		{name: "strong without identity", nature: StrongSnapshot},
		{name: "weak without identity", nature: WeakSnapshot},
		{name: "none with identity", identity: "etag:1", nature: NoSnapshot},
		{name: "invalid nature", identity: "etag:1", nature: SnapshotNature(9)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			snapshot, err := NewSnapshot(test.identity, test.nature)
			if test.valid {
				if err != nil || !snapshot.Valid() {
					t.Fatalf("NewSnapshot(%q, %v) = %#v, %v", test.identity, test.nature, snapshot, err)
				}
				return
			}
			if err == nil || snapshot.Valid() {
				t.Fatalf("NewSnapshot(%q, %v) was accepted as %#v", test.identity, test.nature, snapshot)
			}
		})
	}
}
