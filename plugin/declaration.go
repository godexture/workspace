package plugin

import (
	"errors"
	"fmt"
	"strings"

	"github.com/godexture/godec/media/key"
)

// DeclarationKey identifies one composition namespace and key. The namespace
// comes from a named Go marker type, so independently authored declarations do
// not collide merely because their strings happen to match.
type DeclarationKey struct {
	namespace Identity
	name      string
}

func (k DeclarationKey) Valid() bool { return !k.namespace.IsZero() && k.name != "" }

func (k DeclarationKey) Namespace() Identity { return k.namespace }
func (k DeclarationKey) Name() string        { return k.name }

func (k DeclarationKey) String() string {
	if k.namespace.IsZero() {
		return k.name
	}
	return k.namespace.String() + ":" + k.name
}

// Declaration is composition metadata. A declaration attached through
// Definition.WithDeclarations carries its owning definition identity; a
// declaration added directly to Set is owned by the composition root.
type Declaration struct {
	key      DeclarationKey
	targets  []DeclarationTarget
	problems []string
	owner    Identity
	requires []Declaration
}

// WithVocabulary returns a copy of d that carries the key declarations its
// namespace depends on. Composition expands them alongside d, so a plugin that
// contributes d never has to know which vocabulary it implies, and composing
// that plugin alone cannot leave the declaration missing.
//
// Requirements are expanded one level: a vocabulary declaration describes a
// key, not a further dependency.
func (d Declaration) WithVocabulary(values ...Declaration) Declaration {
	result := d.clone()
	for _, value := range values {
		result.requires = append(result.requires, value.clone())
	}
	return result
}

// expand returns d followed by the vocabulary it requires, all under owner.
func (d Declaration) expand(owner Identity) []Declaration {
	result := make([]Declaration, 0, 1+len(d.requires))
	result = append(result, d.withOwner(owner))
	for _, required := range d.requires {
		result = append(result, required.withOwner(owner))
	}
	return result
}

// Declare constructs a component declaration in the namespace of Namespace.
// It must point at at least one component identity.
func Declare[Namespace any](key string, targets ...Identity) Declaration {
	identity, err := identityOf[Namespace]()
	result := Declaration{key: DeclarationKey{namespace: identity, name: strings.TrimSpace(key)}}
	if err != nil {
		result.problems = append(result.problems, "declaration namespace "+err.Error())
	}
	result.targets = make([]DeclarationTarget, len(targets))
	for index, target := range targets {
		result.targets[index] = componentTarget(target)
	}
	return result
}

// DeclaredKey is the composition view shared by key.Key and property.Key.
type DeclaredKey interface {
	Erased() key.Erased
	Problem() error
}

type keyDeclarationNamespace struct{}

// DeclareKey exposes a typed key to host composition validation. Keys remain
// usable without this declaration; public vocabularies declare them so a host
// can reject one marker associated with different payload types.
func DeclareKey(declared DeclaredKey) Declaration {
	identity, namespaceProblem := identityOf[keyDeclarationNamespace]()
	result := Declaration{key: DeclarationKey{namespace: identity}}
	if namespaceProblem != nil {
		result.problems = append(result.problems, "key declaration namespace "+namespaceProblem.Error())
	}
	if declared == nil {
		result.problems = append(result.problems, "key declaration is nil")
		return result
	}

	erased := declared.Erased()
	result.key.name = erased.ID().String()
	result.targets = []DeclarationTarget{typeTarget(erased.ValueType())}
	if problem := declared.Problem(); problem != nil {
		result.problems = append(result.problems, problem.Error())
	}
	return result
}

func (d Declaration) Valid() bool {
	if len(d.problems) != 0 || !d.key.Valid() || len(d.targets) == 0 {
		return false
	}
	for _, target := range d.targets {
		if !target.Valid() {
			return false
		}
	}
	return true
}

func (d Declaration) Key() DeclarationKey { return d.key }

// Owner returns the definition that contributes this declaration. The zero
// identity means the declaration belongs directly to the composition root.
func (d Declaration) Owner() Identity { return d.owner }

// Targets returns declaration targets in their semantic role order.
func (d Declaration) Targets() []DeclarationTarget {
	return append([]DeclarationTarget(nil), d.targets...)
}

// Problem returns the construction problem, if any.
func (d Declaration) Problem() error {
	problems := append([]string(nil), d.problems...)
	if !d.key.Valid() {
		problems = append(problems, "declaration key is invalid")
	}
	if len(d.targets) == 0 {
		problems = append(problems, "declaration must have at least one target")
	}
	for index, target := range d.targets {
		if !target.Valid() {
			problems = append(problems, fmt.Sprintf("declaration target %d is invalid", index))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	joined := make([]error, len(problems))
	for index, problem := range problems {
		joined[index] = errors.New(problem)
	}
	return errors.Join(joined...)
}

func (d Declaration) SameTargets(other Declaration) bool {
	if len(d.targets) != len(other.targets) {
		return false
	}
	for index, target := range d.targets {
		if target != other.targets[index] {
			return false
		}
	}
	return true
}

func (d Declaration) String() string {
	if !d.Valid() {
		return fmt.Sprintf("invalid declaration %q", d.key.String())
	}
	targets := make([]string, len(d.targets))
	for index, target := range d.targets {
		targets[index] = target.String()
	}
	return d.key.String() + " -> " + strings.Join(targets, ",")
}

func (d Declaration) withOwner(owner Identity) Declaration {
	d = d.clone()
	d.owner = owner
	return d
}

func (d Declaration) clone() Declaration {
	d.targets = append([]DeclarationTarget(nil), d.targets...)
	d.problems = append([]string(nil), d.problems...)
	d.requires = cloneDeclarations(d.requires)
	return d
}

func cloneDeclarations(values []Declaration) []Declaration {
	result := make([]Declaration, len(values))
	for index, value := range values {
		result[index] = value.clone()
	}
	return result
}
