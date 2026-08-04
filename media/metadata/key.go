package metadata

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/godexture/godec/internal/marker"
	"github.com/godexture/godec/internal/snapshot"
)

// ErrKeyType reports a value whose type does not match its key.
var ErrKeyType = errors.New("metadata value has the wrong type for its key")

// KeyID is the stable identity of a metadata key. Core defines no concrete
// key: the shared vocabulary lives in the tag package and third parties define
// their own keys without changing core.
type KeyID struct{ canonical string }

func (id KeyID) IsZero() bool        { return id.canonical == "" }
func (id KeyID) String() string      { return id.canonical }
func (id KeyID) PackagePath() string { return marker.PackagePath(id.canonical) }
func (id KeyID) Name() string        { return marker.Name(id.canonical) }

// Key is a typed metadata key. Reference-valued keys must declare a clone, the
// same rule config codecs and property keys follow, so a document snapshot
// never silently shares mutable state with its producer.
type Key[T any] struct {
	id        KeyID
	valueType reflect.Type
	clone     func(T) T
	problem   string
}

// DefineKey creates an open metadata key in the namespace of Marker.
func DefineKey[Marker any, T any](clones ...func(T) T) Key[T] {
	canonical, err := marker.Canonical[Marker]()
	if err != nil {
		return Key[T]{problem: "metadata " + err.Error()}
	}
	key := Key[T]{id: KeyID{canonical: canonical}, valueType: reflect.TypeFor[T]()}
	if len(clones) > 1 || (len(clones) == 1 && clones[0] == nil) {
		key.problem = "metadata clone must be supplied at most once and must not be nil"
		return key
	}
	switch {
	case len(clones) == 1:
		key.clone = clones[0]
	case snapshot.NeedsClone(key.valueType):
		key.problem = fmt.Sprintf("metadata key %s requires a declared clone for reference-valued type %s", canonical, key.valueType)
	default:
		key.clone = func(value T) T { return value }
	}
	return key
}

// KeyIdentityOf returns a key identity without declaring a key.
func KeyIdentityOf[Marker any]() KeyID {
	canonical, err := marker.Canonical[Marker]()
	if err != nil {
		return KeyID{}
	}
	return KeyID{canonical: canonical}
}

func (k Key[T]) Valid() bool             { return !k.id.IsZero() && k.valueType != nil && k.clone != nil }
func (k Key[T]) ID() KeyID               { return k.id }
func (k Key[T]) ValueType() reflect.Type { return k.valueType }

// Problem returns the key construction problem, if any.
func (k Key[T]) Problem() error {
	if k.problem == "" {
		return nil
	}
	return errors.New(k.problem)
}

// Values returns every value stored under this key, in document order. A key
// may legitimately carry several values, so this never collapses them.
func (k Key[T]) Values(document Document) []T {
	var result []T
	for _, entry := range document.entries {
		if entry.key != k.id || entry.valueType != k.valueType {
			continue
		}
		value, ok := k.cloneAny(entry.value)
		if !ok {
			continue
		}
		result = append(result, value.(T))
	}
	return result
}

// First returns the first value stored under this key.
func (k Key[T]) First(document Document) (T, bool) {
	values := k.Values(document)
	if len(values) == 0 {
		var zero T
		return zero, false
	}
	return values[0], true
}

// Erased returns a key's identity and its declared clone in erased form.
//
// It exists so a package that stores typed values outside a Document, such as
// per-item side data, can reuse one key declaration and its C17 clone rule
// instead of repeating them. One ReplayGain key is then usable both as document
// metadata and as side data. The returned clone accepts only values of the
// key's type and is the only way to snapshot them from outside this package.
func Erased[T any](key Key[T]) (KeyID, func(any) (any, bool), error) {
	if problem := key.Problem(); problem != nil {
		return KeyID{}, nil, problem
	}
	if !key.Valid() {
		return KeyID{}, nil, errors.New("metadata key is not declared")
	}
	return key.id, key.cloneAny, nil
}

// keyLike is the erased view a Builder needs. Its unexported method keeps a
// third party from supplying a key that only claims to be valid.
type keyLike interface {
	ID() KeyID
	ValueType() reflect.Type
	Problem() error
	cloneAny(any) (any, bool)
}

func (k Key[T]) cloneAny(value any) (any, bool) {
	if k.clone == nil {
		return nil, false
	}
	typed, ok := value.(T)
	if !ok {
		return nil, false
	}
	return k.clone(typed), true
}
