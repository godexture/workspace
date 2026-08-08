package plugin

import "strings"

// Need is a structured unmet condition optionally carrying the desired
// descriptor. Code is stable and machine-readable.
type Need[D any] struct {
	code       string
	descriptor D
	hasTarget  bool
}

func DescriptorNeed[D any](code string, descriptor D) Need[D] {
	return Need[D]{code: strings.TrimSpace(code), descriptor: descriptor, hasTarget: true}
}

func ConditionNeed[D any](code string) Need[D] {
	return Need[D]{code: strings.TrimSpace(code)}
}

func (n Need[D]) Valid() bool  { return n.code != "" }
func (n Need[D]) Code() string { return n.code }
func (n Need[D]) Desired() (D, bool) {
	return n.descriptor, n.hasTarget
}

// Requirement associates an unmet Need with one component input port.
type Requirement[D any] struct {
	port string
	need Need[D]
}

func Require[D any](port string, need Need[D]) Requirement[D] {
	return Requirement[D]{port: strings.TrimSpace(port), need: need}
}

func (r Requirement[D]) Valid() bool   { return r.port != "" && r.need.Valid() }
func (r Requirement[D]) Port() string  { return r.port }
func (r Requirement[D]) Need() Need[D] { return r.need }
