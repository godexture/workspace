package job

import (
	"math"
	"time"

	"github.com/godexture/godec/access"
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

const (
	defaultScratchMaxBytes resource.Bytes = 64 << 20
	// defaultTemporaryMaxBytes is generous because it sets nothing aside: an
	// offline conversion that has to hold a stream to find its end should not
	// fail on an ordinary recording, and the ceiling is here to stop a runaway
	// job rather than to ration ordinary ones.
	defaultTemporaryMaxBytes resource.Bytes = 4 << 30
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

// MetadataPolicy says what a job makes of metadata a carrier cannot hold as the
// document stated it.
type MetadataPolicy uint8

const (
	// PreserveMetadata writes what the carrier can hold and reports the rest.
	// What a carrier can say is a fact about the carrier, so refusing the whole
	// conversion over it would make ordinary work impossible.
	PreserveMetadata MetadataPolicy = iota + 1
	// StrictMetadata refuses to plan a conversion that would lose any of it.
	// It is opt-in because the answer it wants -- nothing lost -- is not
	// available from every pair of formats.
	StrictMetadata
)

func (p MetadataPolicy) Valid() bool { return p >= PreserveMetadata && p <= StrictMetadata }

func (p MetadataPolicy) String() string {
	if p == StrictMetadata {
		return "strict"
	}
	return "preserve"
}

// ResourcePolicy bounds planning-visible coarse resources. A zero Limit with
// Limited false means that the Host decides the grant during preparation.
type ResourcePolicy struct {
	Limited bool
	Limit   resource.Grant
	Queue   QueuePolicy
	// ScratchMaxBytes is the aggregate fixed ceiling shared by node-local
	// scratch journals and selected output spools. Zero disables both.
	ScratchMaxBytes resource.Bytes
	// TemporaryMaxBytes bounds what node-local stores that grow rather than
	// reserve may use altogether. It sets nothing aside: a node that cannot
	// state its size in advance is charged as it writes. Zero disables them.
	TemporaryMaxBytes resource.Bytes
	// TemporaryUnlimited lifts that ceiling, leaving the storage itself as the
	// only bound. It is a choice a job makes rather than one a component can
	// make for it.
	TemporaryUnlimited bool
	AllowSpool         bool
	SpoolMaxBytes      resource.Bytes
	SpoolStorage       access.SpoolStorage
}

// QueuePolicy selects the per-edge physical bounds fixed into the executable
// Plan. Bytes and Span are applied only when the connected schema supplies the
// corresponding inexpensive trait. Span is converted to stream-local ticks
// during planning, never in the item loop.
type QueuePolicy struct {
	Items int
	Bytes resource.Bytes
	Span  time.Duration
}

func (p QueuePolicy) Valid() bool {
	return p.validItems() && p.validBytes() && p.validSpan()
}

func (p QueuePolicy) validItems() bool { return p.Items > 0 }
func (p QueuePolicy) validBytes() bool { return uint64(p.Bytes) <= math.MaxInt64 }
func (p QueuePolicy) validSpan() bool  { return p.Span >= 0 }

// AlignmentPolicy selects timestamp tolerance for Zip fan-in. A zero value
// disables Zip alignment tolerance; other fan-in policies ignore it.
type AlignmentPolicy struct {
	Zip time.Duration
}

func (p AlignmentPolicy) Valid() bool { return p.Zip >= 0 }

func (p ResourcePolicy) Valid() bool {
	return p.Queue.Valid() && p.validScratch() && p.validTemporary() && p.validSpool()
}

func (p ResourcePolicy) validScratch() bool {
	return uint64(p.ScratchMaxBytes) <= math.MaxInt64
}

func (p ResourcePolicy) validTemporary() bool {
	return uint64(p.TemporaryMaxBytes) <= math.MaxInt64
}

func (p ResourcePolicy) validSpool() bool {
	if !p.AllowSpool {
		return p.SpoolMaxBytes == 0 && p.SpoolStorage == 0
	}
	return p.SpoolMaxBytes > 0 && uint64(p.SpoolMaxBytes) <= math.MaxInt64 && p.SpoolStorage.Valid() && p.ScratchMaxBytes >= p.SpoolMaxBytes
}

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
	Metadata       MetadataPolicy
	Alignment      AlignmentPolicy
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
	if !p.Metadata.Valid() {
		items = append(items, policyDiagnostic("job.invalid-policy-metadata", "policy metadata behavior is invalid", "metadata"))
	}
	queue := p.Resources.Queue
	if !queue.validItems() {
		items = append(items, policyDiagnostic("job.invalid-policy-queue-items", "queue item limit must be positive", "resources", "queue", "items"))
	}
	if !queue.validBytes() {
		items = append(items, policyDiagnostic("job.invalid-policy-queue-bytes", "queue byte limit exceeds the runtime range", "resources", "queue", "bytes"))
	}
	if !queue.validSpan() {
		items = append(items, policyDiagnostic("job.invalid-policy-queue-span", "queue span must not be negative", "resources", "queue", "span"))
	}
	if !p.Alignment.Valid() {
		items = append(items, policyDiagnostic("job.invalid-policy-alignment-zip", "zip alignment tolerance must not be negative", "alignment", "zip"))
	}
	if !p.Resources.validSpool() {
		items = append(items, policyDiagnostic("job.invalid-policy-spool", "spool policy requires an explicit positive byte limit and storage when enabled, neither when disabled, and an aggregate scratch limit at least as large", "resources", "spool"))
	}
	if !p.Resources.validScratch() {
		items = append(items, policyDiagnostic("job.invalid-policy-scratch", "aggregate scratch byte limit exceeds the runtime range", "resources", "scratch"))
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
		Metadata:       PreserveMetadata,
		Alignment:      AlignmentPolicy{},
		Resources:      ResourcePolicy{Queue: QueuePolicy{Items: 4}, ScratchMaxBytes: defaultScratchMaxBytes, TemporaryMaxBytes: defaultTemporaryMaxBytes},
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
		policy.Alignment = AlignmentPolicy{Zip: 250 * time.Millisecond}
		policy.Resources.Queue = QueuePolicy{Items: 2, Bytes: 16 << 20, Span: 250 * time.Millisecond}
		policy.Resources.ScratchMaxBytes = 0
		policy.Resources.TemporaryMaxBytes = 0
	default:
		return Policy{}, false
	}
	return policy, true
}

func defaultPolicy() Policy {
	policy, _ := PolicyFor(Fast)
	return policy
}
