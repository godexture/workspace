package testkit

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/godexture/godec/plugin"
)

// Coverage records identities actually passed through the common typed-case
// runner. It is explicit test state, not a process-global registration hook.
type Coverage struct {
	mu        sync.Mutex
	executed  map[plugin.Identity]int
	uncovered map[string]string
}

// NewCoverage creates an empty typed-case execution registry.
func NewCoverage() *Coverage {
	return &Coverage{
		executed:  make(map[plugin.Identity]int),
		uncovered: make(map[string]string),
	}
}

// UncoveredContract identifies one conformance contract that has an explicit
// future owner instead of a helper or executed case in the current milestone.
type UncoveredContract struct {
	Identity  string
	Milestone string
}

// AssignUncovered records one intentionally uncovered contract and the
// milestone responsible for closing it. Empty and repeated identities are
// rejected so absence cannot silently masquerade as an assignment.
func (c *Coverage) AssignUncovered(identity, milestone string) error {
	identity = strings.TrimSpace(identity)
	milestone = strings.TrimSpace(milestone)
	if c == nil {
		return fmt.Errorf("testkit typed coverage: nil registry cannot assign uncovered contract %q", identity)
	}
	if identity == "" {
		return fmt.Errorf("testkit typed coverage: uncovered contract identity is required")
	}
	if milestone == "" {
		return fmt.Errorf("testkit typed coverage: uncovered contract %s has no responsible milestone", identity)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.uncovered == nil {
		c.uncovered = make(map[string]string)
	}
	if assigned, exists := c.uncovered[identity]; exists {
		return fmt.Errorf("testkit typed coverage: uncovered contract %s is already assigned to %s", identity, assigned)
	}
	c.uncovered[identity] = milestone
	return nil
}

// Uncovered returns assigned gaps in stable contract-identity order.
func (c *Coverage) Uncovered() []UncoveredContract {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	result := make([]UncoveredContract, 0, len(c.uncovered))
	for identity, milestone := range c.uncovered {
		result = append(result, UncoveredContract{Identity: identity, Milestone: milestone})
	}
	c.mu.Unlock()
	sort.Slice(result, func(left, right int) bool {
		return result[left].Identity < result[right].Identity
	})
	return result
}

// VerifyComplete is the final-state gate used when every public conformance
// contract must have a helper and an executed case. Milestone-local tests may
// inspect Uncovered instead while their assigned gaps intentionally remain.
func (c *Coverage) VerifyComplete(t testing.TB) {
	t.Helper()
	if err := c.completionError(); err != nil {
		t.Error(err)
	}
}

func (c *Coverage) completionError() error {
	if c == nil {
		return fmt.Errorf("testkit typed coverage: registry is nil")
	}
	gaps := c.Uncovered()
	if len(gaps) == 0 {
		return nil
	}
	values := make([]string, len(gaps))
	for index, gap := range gaps {
		values[index] = gap.Identity + " (" + gap.Milestone + ")"
	}
	return fmt.Errorf("testkit typed coverage: uncovered contracts remain: %s", strings.Join(values, ", "))
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
	for _, problem := range c.executableProblems(set) {
		t.Error(problem)
	}
}

func (c *Coverage) executableProblems(set plugin.Set) []error {
	if c == nil {
		return []error{fmt.Errorf("testkit typed coverage: registry is nil")}
	}
	c.mu.Lock()
	executed := make(map[plugin.Identity]int, len(c.executed))
	for identity, count := range c.executed {
		executed[identity] = count
	}
	c.mu.Unlock()

	known := make(map[plugin.Identity]bool)
	var problems []error
	for _, component := range set.Components() {
		known[component.Identity()] = component.View().Executable
		if component.View().Executable && executed[component.Identity()] == 0 {
			problems = append(problems, fmt.Errorf("testkit typed coverage: executable component %s has no executed typed case", component.Identity()))
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
		problems = append(problems, fmt.Errorf("testkit typed coverage: executed identity %s is absent from the covered Set", identity))
	}
	for identity, count := range executed {
		if count < 1 {
			problems = append(problems, fmt.Errorf("testkit typed coverage: invalid execution count for %s: %s", identity, fmt.Sprint(count)))
		}
	}
	return problems
}

// VerifyIdentities fails unless every listed control-plane or executable
// component belongs to set and completed at least one typed case. It is the
// explicit coverage gate for trait-only components excluded from the
// executable population.
func (c *Coverage) VerifyIdentities(t testing.TB, set plugin.Set, identities ...plugin.Identity) {
	t.Helper()
	if c == nil {
		t.Error("testkit typed coverage: registry is nil")
		return
	}
	c.mu.Lock()
	executed := make(map[plugin.Identity]int, len(c.executed))
	for identity, count := range c.executed {
		executed[identity] = count
	}
	c.mu.Unlock()
	known := make(map[plugin.Identity]struct{})
	for _, component := range set.Components() {
		known[component.Identity()] = struct{}{}
	}
	seen := make(map[plugin.Identity]struct{}, len(identities))
	for _, identity := range identities {
		if identity.IsZero() {
			t.Error("testkit typed coverage: required identity is empty")
			continue
		}
		if _, duplicate := seen[identity]; duplicate {
			t.Errorf("testkit typed coverage: required identity %s is repeated", identity)
			continue
		}
		seen[identity] = struct{}{}
		if _, ok := known[identity]; !ok {
			t.Errorf("testkit typed coverage: required identity %s is absent from the covered Set", identity)
			continue
		}
		if executed[identity] == 0 {
			t.Errorf("testkit typed coverage: component %s has no executed typed case", identity)
		}
	}
}
