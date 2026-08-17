package flow

import (
	"errors"
	"fmt"
)

// PortDescriptor binds one stream descriptor to a component port. Several
// bindings with the same port ID represent an ordered many-port input or
// output.
type PortDescriptor[D any] struct {
	port       string
	descriptor D
}

// Describe creates a descriptor binding. Validation is deferred to
// Descriptors.Validate so Compile can return all bindings as one value.
func Describe[D any](port string, descriptor D) PortDescriptor[D] {
	return PortDescriptor[D]{port: port, descriptor: descriptor}
}

func (b PortDescriptor[D]) Port() string  { return b.port }
func (b PortDescriptor[D]) Descriptor() D { return b.descriptor }
func (b PortDescriptor[D]) Valid() bool   { return b.port != "" }

// Descriptors is an immutable ordered collection used at the Ports/Compile
// boundary. It deliberately is not a map: order on a many port can be
// semantic, and map iteration must not affect planning.
type Descriptors[D any] struct{ bindings []PortDescriptor[D] }

func NewDescriptors[D any](bindings ...PortDescriptor[D]) Descriptors[D] {
	return Descriptors[D]{bindings: append([]PortDescriptor[D](nil), bindings...)}
}

func (d Descriptors[D]) Len() int { return len(d.bindings) }

func (d Descriptors[D]) Bindings() []PortDescriptor[D] {
	return append([]PortDescriptor[D](nil), d.bindings...)
}

// At returns descriptors bound to port in their declared order.
func (d Descriptors[D]) At(port string) []D {
	var result []D
	for _, binding := range d.bindings {
		if binding.port == port {
			result = append(result, binding.descriptor)
		}
	}
	return result
}

// One returns the sole descriptor on port. It reports false for absent and
// many-valued ports so a component cannot silently choose one input.
func (d Descriptors[D]) One(port string) (D, bool) {
	values := d.At(port)
	if len(values) != 1 {
		var zero D
		return zero, false
	}
	return values[0], true
}

func (d Descriptors[D]) Validate(validate func(D) bool) error {
	var problems []error
	for index, binding := range d.bindings {
		if binding.port == "" {
			problems = append(problems, fmt.Errorf("descriptor binding %d has no port ID", index))
		}
		if validate != nil && !validate(binding.descriptor) {
			problems = append(problems, fmt.Errorf("descriptor binding %d has an invalid stream descriptor", index))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.Join(problems...)
}
