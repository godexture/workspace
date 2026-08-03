package plugin

import (
	"fmt"
	"strings"
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

// Declaration is a composition-time declaration owned by plugin.Set. Its
// targets are component identities; the declaration does not import or know
// the package that defined the domain-specific meaning of the key.
type Declaration struct {
	key     DeclarationKey
	targets []Identity
}

// Declare constructs a declaration in the namespace of Namespace. A
// declaration must point at at least one component identity.
func Declare[Namespace any](key string, targets ...Identity) Declaration {
	identity, _ := identityOf[Namespace]()
	result := Declaration{key: DeclarationKey{namespace: identity, name: strings.TrimSpace(key)}}
	result.targets = append([]Identity(nil), targets...)
	return result
}

func (d Declaration) Valid() bool {
	if !d.key.Valid() || len(d.targets) == 0 {
		return false
	}
	for _, target := range d.targets {
		if target.IsZero() {
			return false
		}
	}
	return true
}

func (d Declaration) Key() DeclarationKey { return d.key }

// Targets returns component identities in the declaration's role order.
func (d Declaration) Targets() []Identity { return append([]Identity(nil), d.targets...) }

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
