package job

import (
	"math"
	"time"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/resource"
)

// Preset is user-facing shorthand expanded before component compilation.
type Preset uint8

const (
	Fast Preset = iota + 1
	Stable
	Portable
	Realtime
)

func (p Preset) Valid() bool { return p >= Fast && p <= Realtime }

func (p Preset) String() string {
	switch p {
	case Fast:
		return "fast"
	case Stable:
		return "stable"
	case Portable:
		return "portable"
	case Realtime:
		return "realtime"
	default:
		return "unknown"
	}
}

type Goal uint8

const (
	ThroughputGoal Goal = iota + 1
	LatencyGoal
	MemoryGoal
)

func (g Goal) Valid() bool { return g >= ThroughputGoal && g <= MemoryGoal }

type Accuracy uint8

const (
	ExactAccuracy Accuracy = iota + 1
	BoundedAccuracy
)

func (a Accuracy) Valid() bool { return a == ExactAccuracy || a == BoundedAccuracy }

type Repeatability uint8

const (
	Repeatable Repeatability = iota + 1
	Variable
)

func (r Repeatability) Valid() bool { return r == Repeatable || r == Variable }

type ArtifactPolicy uint8

const (
	ArtifactNone ArtifactPolicy = iota + 1
	ArtifactStable
	ArtifactPortable
)

func (a ArtifactPolicy) Valid() bool { return a >= ArtifactNone && a <= ArtifactPortable }

// ImplementationPolicy declares which implementation mechanisms may be
// selected. It does not imply that a matching implementation exists.
type ImplementationPolicy struct {
	PureGo   bool
	Unsafe   bool
	SIMD     bool
	FMA      bool
	Parallel bool
	Native   bool
	Device   bool
}

func (p ImplementationPolicy) Valid() bool { return p.PureGo || p.Native || p.Device }

type ContinuityPolicy uint8

const (
	PreserveContinuity ContinuityPolicy = iota + 1
	DropLateContinuity
	ConcealContinuity
)

func (p ContinuityPolicy) Valid() bool {
	return p >= PreserveContinuity && p <= ConcealContinuity
}

// ResourcePolicy bounds planning-visible coarse resources. A zero Limit with
// Limited false means that the Host decides the grant during preparation.
type ResourcePolicy struct {
	Limited    bool
	Limit      resource.Grant
	Queue      QueuePolicy
	AllowSpool bool
}

// QueuePolicy selects the per-edge bounds fixed into the executable Plan.
// Bytes and Window are applied only when the connected schema supplies the
// corresponding inexpensive trait. Window is converted to stream-local ticks
// during planning, never in the item loop.
type QueuePolicy struct {
	Items  int
	Bytes  resource.Bytes
	Window time.Duration
}

func (p QueuePolicy) Valid() bool {
	return p.validItems() && p.validBytes() && p.validWindow()
}

func (p QueuePolicy) validItems() bool  { return p.Items > 0 }
func (p QueuePolicy) validBytes() bool  { return uint64(p.Bytes) <= math.MaxInt64 }
func (p QueuePolicy) validWindow() bool { return p.Window >= 0 }

func (p ResourcePolicy) Valid() bool { return p.Queue.Valid() }

// Policy is the expanded vector consumed by the planner. Preset is retained
// only so a Plan can explain which user-facing shorthand produced the vector.
type Policy struct {
	Preset         Preset
	Goal           Goal
	Accuracy       Accuracy
	Repeatability  Repeatability
	Artifact       ArtifactPolicy
	Implementation ImplementationPolicy
	Continuity     ContinuityPolicy
	Resources      ResourcePolicy
}

func (p Policy) Valid() bool {
	return len(p.diagnostics()) == 0
}

func (p Policy) diagnostics() (items []diagnostic.Item) {
	if !p.Preset.Valid() {
		items = append(items, policyDiagnostic("job.invalid-policy-preset", "policy preset is invalid", "preset"))
	}
	if !p.Goal.Valid() {
		items = append(items, policyDiagnostic("job.invalid-policy-goal", "policy goal is invalid", "goal"))
	}
	if !p.Accuracy.Valid() {
		items = append(items, policyDiagnostic("job.invalid-policy-accuracy", "policy accuracy is invalid", "accuracy"))
	}
	if !p.Repeatability.Valid() {
		items = append(items, policyDiagnostic("job.invalid-policy-repeatability", "policy repeatability is invalid", "repeatability"))
	}
	if !p.Artifact.Valid() {
		items = append(items, policyDiagnostic("job.invalid-policy-artifact", "policy artifact requirement is invalid", "artifact"))
	}
	if !p.Implementation.Valid() {
		items = append(items, policyDiagnostic("job.invalid-policy-implementation", "policy permits no implementation mechanism", "implementation"))
	}
	if !p.Continuity.Valid() {
		items = append(items, policyDiagnostic("job.invalid-policy-continuity", "policy continuity behavior is invalid", "continuity"))
	}
	queue := p.Resources.Queue
	if !queue.validItems() {
		items = append(items, policyDiagnostic("job.invalid-policy-queue-items", "queue item limit must be positive", "resources", "queue", "items"))
	}
	if !queue.validBytes() {
		items = append(items, policyDiagnostic("job.invalid-policy-queue-bytes", "queue byte limit exceeds the runtime range", "resources", "queue", "bytes"))
	}
	if !queue.validWindow() {
		items = append(items, policyDiagnostic("job.invalid-policy-queue-window", "queue window must not be negative", "resources", "queue", "window"))
	}
	return items
}

func policyDiagnostic(code, message string, fields ...string) diagnostic.Item {
	return diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.FieldPath(append([]string{"policy"}, fields...)...), message, nil)
}

// PolicyFor expands a named preset into an explicit policy vector.
func PolicyFor(preset Preset) (Policy, bool) {
	implementation := ImplementationPolicy{PureGo: true, Unsafe: true, SIMD: true, FMA: true, Parallel: true}
	policy := Policy{
		Preset:         preset,
		Goal:           ThroughputGoal,
		Accuracy:       BoundedAccuracy,
		Repeatability:  Repeatable,
		Artifact:       ArtifactNone,
		Implementation: implementation,
		Continuity:     PreserveContinuity,
		Resources:      ResourcePolicy{Queue: QueuePolicy{Items: 4}},
	}
	switch preset {
	case Fast:
	case Stable:
		policy.Artifact = ArtifactStable
	case Portable:
		policy.Artifact = ArtifactPortable
		policy.Implementation = ImplementationPolicy{PureGo: true}
	case Realtime:
		policy.Goal = LatencyGoal
		policy.Resources.Queue = QueuePolicy{Items: 2, Bytes: 16 << 20, Window: 250 * time.Millisecond}
	default:
		return Policy{}, false
	}
	return policy, true
}

func defaultPolicy() Policy {
	policy, _ := PolicyFor(Fast)
	return policy
}
