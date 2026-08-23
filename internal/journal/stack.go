package journal

import "bytes"

// StackID names one interned stack. Zero means no stack was kept, which
// Truncated separates into "there was none" and "there was one and the budget
// dropped it".
type StackID uint32

// NoStack is the identity of "this failure kept no stack".
const NoStack StackID = 0

// depot interns stacks.
//
// A storm of releases failing at one call site produces one stack repeated
// thousands of times. Storing it once and referring to it does two jobs: it
// bounds what the repetition costs, and it gives the call site a stable
// identity that a failure class can be keyed on without going anywhere near
// what an error says.
type depot struct {
	budget Budget
	ids    map[uint64][]StackID
	stacks [][]byte
	bytes  int
	// dropped counts the stacks the budget refused, so a reader can tell a
	// failure that never had a stack from one whose stack was not kept.
	dropped uint64
}

func newDepot(budget Budget) *depot {
	return &depot{budget: budget, ids: make(map[uint64][]StackID)}
}

// intern returns the identity of stack, storing it if it is new and the budget
// allows. The second result reports whether the stack is retained: a false
// with a non-empty input is a stack the budget dropped.
func (d *depot) intern(stack []byte) (StackID, bool) {
	stack = callSite(stack)
	if len(stack) == 0 {
		return NoStack, true
	}
	digest := fingerprint(stack)
	for _, id := range d.ids[digest] {
		if bytes.Equal(d.stacks[id-1], stack) {
			return id, true
		}
	}
	if len(d.stacks) >= d.budget.Stacks || d.bytes+len(stack) > d.budget.StackBytes {
		d.dropped = add(d.dropped, 1)
		return NoStack, false
	}
	stored := append([]byte(nil), stack...)
	d.stacks = append(d.stacks, stored)
	id := StackID(len(d.stacks))
	d.ids[digest] = append(d.ids[digest], id)
	d.bytes += len(stored)
	return id, true
}

// at returns the interned bytes without copying them. Every failure that
// shares a call site shares this one array; nothing mutates a recorded stack.
func (d *depot) at(id StackID) []byte {
	if id == NoStack || int(id) > len(d.stacks) {
		return nil
	}
	return d.stacks[id-1]
}

// fingerprint is FNV-1a over the length and a bounded prefix.
//
// It only has to spread well, not to be unique: a match is confirmed by
// comparing the bytes, so a collision costs one memcmp and never a wrong
// answer. Bounding what is hashed keeps the cost of a storm proportional to the
// number of failures rather than to how deep the stacks happen to be, and a
// stack's innermost frames are where two call sites differ anyway.
const fingerprintPrefix = 256

func fingerprint(value []byte) uint64 {
	const offset, prime = uint64(14695981039346656037), uint64(1099511628211)
	digest := offset ^ uint64(len(value))
	digest *= prime
	if len(value) > fingerprintPrefix {
		value = value[:fingerprintPrefix]
	}
	for _, piece := range value {
		digest ^= uint64(piece)
		digest *= prime
	}
	return digest
}

// callSite drops the goroutine header a captured stack begins with.
//
// It names neither the call site nor anything a reader can act on: the number
// is fresh every run, and the bracketed status is whatever the scheduler
// happened to be doing. A stack captured while the collector is scanning that
// goroutine reads "running (scan)" where the same call site otherwise reads
// "running", so keeping the header would split one call site into two classes
// depending on when the collector ran -- and a storm that splits into enough
// classes crowds the failure that explains the run out of the ledger.
//
// The frames are what identify a call site, and they are the same whichever
// goroutine reached it.
func callSite(stack []byte) []byte {
	if !bytes.HasPrefix(stack, goroutineHeader) {
		return stack
	}
	if index := bytes.IndexByte(stack, '\n'); index >= 0 {
		return stack[index+1:]
	}
	return stack
}

var goroutineHeader = []byte("goroutine ")
