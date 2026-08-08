package access

import (
	"errors"
	"sort"
	"strings"
)

// Capability is comparable declaration data. It stays separate from the
// narrow view interfaces because requirements and Plans must be inert values.
type Capability string

const (
	SequentialRead Capability = "sequential-read"
	RandomRead     Capability = "random-read"
	StableSize     Capability = "stable-size"
	Reopen         Capability = "reopen"
	ConcurrentRead Capability = "concurrent-read"
	CancelableRead Capability = "cancelable-read"
)

func (c Capability) Valid() bool { return c != "" && strings.TrimSpace(string(c)) == string(c) }

type Alternative struct{ Capabilities []Capability }

func AnyOf(capabilities ...Capability) Alternative {
	return Alternative{Capabilities: append([]Capability(nil), capabilities...)}
}

func (a Alternative) Clone() Alternative { return AnyOf(a.Capabilities...) }

func (a Alternative) Valid() bool {
	if len(a.Capabilities) == 0 {
		return false
	}
	seen := make(map[Capability]struct{}, len(a.Capabilities))
	for _, capability := range a.Capabilities {
		if !capability.Valid() {
			return false
		}
		if _, exists := seen[capability]; exists {
			return false
		}
		seen[capability] = struct{}{}
	}
	return true
}

type Requirements struct{ Alternatives []Alternative }

func NewRequirements(alternatives ...Alternative) Requirements {
	result := Requirements{Alternatives: make([]Alternative, len(alternatives))}
	for index, alternative := range alternatives {
		capabilities := append([]Capability(nil), alternative.Capabilities...)
		sort.Slice(capabilities, func(left, right int) bool { return capabilities[left] < capabilities[right] })
		result.Alternatives[index] = AnyOf(capabilities...)
	}
	return result
}

func (r Requirements) Empty() bool { return len(r.Alternatives) == 0 }

func (r Requirements) Valid() bool {
	if r.Empty() {
		return false
	}
	for _, alternative := range r.Alternatives {
		if !alternative.Valid() {
			return false
		}
	}
	return true
}

func (r Requirements) Clone() Requirements { return NewRequirements(r.Alternatives...) }

// Capabilities is a canonical set guaranteed by one bound source.
type Capabilities struct{ values []Capability }

var ErrInvalidCapabilities = errors.New("access capabilities are invalid")

func NewCapabilities(values ...Capability) (Capabilities, error) {
	result := append([]Capability(nil), values...)
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	for index, value := range result {
		if !value.Valid() || index != 0 && value == result[index-1] {
			return Capabilities{}, ErrInvalidCapabilities
		}
	}
	return Capabilities{values: result}, nil
}

func (c Capabilities) Valid() bool {
	previous := Capability("")
	for index, value := range c.values {
		if !value.Valid() || index != 0 && value <= previous {
			return false
		}
		previous = value
	}
	return true
}

func (c Capabilities) Values() []Capability { return append([]Capability(nil), c.values...) }

func (c Capabilities) Contains(value Capability) bool {
	index := sort.Search(len(c.values), func(index int) bool { return c.values[index] >= value })
	return index < len(c.values) && c.values[index] == value
}

// Selection is the one capability alternative chosen before component Open.
// It later controls which narrow source view Prepare may hand to a Format.
type Selection struct{ capabilities []Capability }

func (s Selection) Valid() bool { return len(s.capabilities) != 0 }
func (s Selection) Capabilities() []Capability {
	return append([]Capability(nil), s.capabilities...)
}

// Select returns the first declared alternative satisfied by the canonical
// available set. Alternative order is semantic preference; capability order
// inside the selected alternative is canonicalized.
func Select(available Capabilities, requirements Requirements) (Selection, bool) {
	if !available.Valid() || !requirements.Valid() {
		return Selection{}, false
	}
	for _, alternative := range requirements.Alternatives {
		satisfied := true
		for _, capability := range alternative.Capabilities {
			if !available.Contains(capability) {
				satisfied = false
				break
			}
		}
		if !satisfied {
			continue
		}
		selected := append([]Capability(nil), alternative.Capabilities...)
		sort.Slice(selected, func(left, right int) bool { return selected[left] < selected[right] })
		return Selection{capabilities: selected}, true
	}
	return Selection{}, false
}
