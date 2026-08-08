package plugin

import "sort"

type AccuracyContract uint8

const (
	ExactContract AccuracyContract = iota + 1
	BoundedContract
)

func (c AccuracyContract) Valid() bool { return c == ExactContract || c == BoundedContract }

type RepeatabilityContract uint8

const (
	RepeatableContract RepeatabilityContract = iota + 1
	VariableContract
)

func (c RepeatabilityContract) Valid() bool {
	return c == RepeatableContract || c == VariableContract
}

// ArtifactSupport is the strongest byte-reproducibility domain guaranteed by
// an implementation.
type ArtifactSupport uint8

const (
	NoArtifactSupport ArtifactSupport = iota + 1
	StableArtifactSupport
	PortableArtifactSupport
)

func (s ArtifactSupport) Valid() bool {
	return s >= NoArtifactSupport && s <= PortableArtifactSupport
}

// ImplementationKind describes mechanisms used by an implementation.
type ImplementationKind uint32

const (
	PureGoImplementation ImplementationKind = 1 << iota
	UnsafeImplementation
	SIMDImplementation
	FMAImplementation
	ParallelImplementation
	NativeImplementation
	DeviceImplementation
)

const knownImplementations = PureGoImplementation | UnsafeImplementation | SIMDImplementation | FMAImplementation | ParallelImplementation | NativeImplementation | DeviceImplementation

func (k ImplementationKind) Valid() bool { return k != 0 && k&^knownImplementations == 0 }
func (k ImplementationKind) Has(value ImplementationKind) bool {
	return k&value == value
}

// PlatformRequirement is empty when the implementation is portable across
// every platform on which it is built. Features are canonicalized.
type PlatformRequirement struct {
	OS       string
	Arch     string
	Features []string
}

// Contract describes the sole implementation behind a Spec. Explicit
// multi-variant selection follows with the first real variant consumer.
type Contract struct {
	Accuracy       AccuracyContract
	Repeatability  RepeatabilityContract
	Artifact       ArtifactSupport
	Implementation ImplementationKind
	Platform       PlatformRequirement
}

func DefaultContract() Contract {
	return Contract{
		Accuracy:       ExactContract,
		Repeatability:  RepeatableContract,
		Artifact:       NoArtifactSupport,
		Implementation: PureGoImplementation,
	}
}

func (c Contract) Valid() bool {
	if !c.Accuracy.Valid() || !c.Repeatability.Valid() || !c.Artifact.Valid() || !c.Implementation.Valid() {
		return false
	}
	previous := ""
	for _, feature := range c.Platform.Features {
		if feature == "" || feature <= previous {
			return false
		}
		previous = feature
	}
	return true
}

func normalizeContract(contract Contract) Contract {
	if contract.Accuracy == 0 && contract.Repeatability == 0 && contract.Artifact == 0 && contract.Implementation == 0 && contract.Platform.OS == "" && contract.Platform.Arch == "" && len(contract.Platform.Features) == 0 {
		return DefaultContract()
	}
	contract.Platform.Features = append([]string(nil), contract.Platform.Features...)
	sort.Strings(contract.Platform.Features)
	return contract
}

func (c Contract) clone() Contract {
	c.Platform.Features = append([]string(nil), c.Platform.Features...)
	return c
}
