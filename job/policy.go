package job

import "github.com/godexture/godec/resource"

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
	AllowSpool bool
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
	Resources      ResourcePolicy
}

func (p Policy) Valid() bool {
	return p.Preset.Valid() && p.Goal.Valid() && p.Accuracy.Valid() && p.Repeatability.Valid() && p.Artifact.Valid() && p.Implementation.Valid() && p.Continuity.Valid()
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
	default:
		return Policy{}, false
	}
	return policy, true
}

func defaultPolicy() Policy {
	policy, _ := PolicyFor(Fast)
	return policy
}
