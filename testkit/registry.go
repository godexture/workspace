package testkit

import (
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/godexture/godec/plugin"
)

// Coverage records identities actually passed through the common typed-case
// runner. It is explicit test state, not a process-global registration hook.
type Coverage struct {
	mu       sync.Mutex
	executed map[plugin.Identity]int
}

// NewCoverage creates an empty typed-case execution registry.
func NewCoverage() *Coverage {
	return &Coverage{executed: make(map[plugin.Identity]int)}
}

// Track returns an otherwise identical Subject whose completed cases are
// recorded in coverage.
func Track[I, O any](subject Subject[I, O], coverage *Coverage) Subject[I, O] {
	subject.coverage = coverage
	return subject
}

func (c *Coverage) record(identity plugin.Identity) {
	if c == nil || identity.IsZero() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.executed == nil {
		c.executed = make(map[plugin.Identity]int)
	}
	c.executed[identity]++
}

// VerifyExecutable fails unless every executable component in set ran at
// least one typed case, and every recorded identity belongs to set.
func (c *Coverage) VerifyExecutable(t testing.TB, set plugin.Set) {
	t.Helper()
	c.mu.Lock()
	executed := make(map[plugin.Identity]int, len(c.executed))
	for identity, count := range c.executed {
		executed[identity] = count
	}
	c.mu.Unlock()

	known := make(map[plugin.Identity]bool)
	for _, component := range set.Components() {
		known[component.Identity()] = component.View().Executable
		if component.View().Executable && executed[component.Identity()] == 0 {
			t.Errorf("testkit typed coverage: executable component %s has no executed typed case", component.Identity())
		}
	}
	var unknown []string
	for identity := range executed {
		if _, ok := known[identity]; !ok {
			unknown = append(unknown, identity.String())
		}
	}
	sort.Strings(unknown)
	for _, identity := range unknown {
		t.Errorf("testkit typed coverage: executed identity %s is absent from the covered Set", identity)
	}
	for identity, count := range executed {
		if count < 1 {
			t.Errorf("testkit typed coverage: invalid execution count for %s: %s", identity, fmt.Sprint(count))
		}
	}
}
