// Package side defines immutable packet and frame side data.
package side

import "github.com/godexture/godec/media/metadata"

// Data is an immutable ordered set of typed values attached to one item.
//
// It shares metadata.Key, and therefore the C17 clone rule, so a third-party
// reference-valued key must declare its clone exactly once. It does not share
// metadata.Document: a document is a control-plane description of a stream or
// asset, with a scope, carrier origins, and raw blocks, none of which mean
// anything for a value attached to a single packet or frame.
//
// The zero value carries no allocation and no indirection, so an item without
// side data pays nothing for the field.
type Data struct{ values []value }

type value struct {
	key    metadata.KeyID
	stored any
	clone  func(any) (any, bool)
}

// Valid reports whether any value is attached.
func (d Data) Valid() bool { return len(d.values) > 0 }

// Empty reports whether no value is attached.
func (d Data) Empty() bool { return len(d.values) == 0 }

// Len reports how many values are attached.
func (d Data) Len() int { return len(d.values) }

// Keys returns the attached key identities in insertion order.
func (d Data) Keys() []metadata.KeyID {
	if len(d.values) == 0 {
		return nil
	}
	result := make([]metadata.KeyID, len(d.values))
	for index, entry := range d.values {
		result[index] = entry.key
	}
	return result
}

// Add returns a copy with one typed value appended. The receiver is unchanged,
// so side data already handed to a downstream item cannot gain values behind
// its back.
func Add[T any](data Data, key metadata.Key[T], item T) (Data, error) {
	id, clone, err := metadata.Erased(key)
	if err != nil {
		return Data{}, err
	}
	// item is already of the key's type, so the clone cannot reject it.
	stored, _ := clone(item)
	result := Data{values: make([]value, len(data.values), len(data.values)+1)}
	copy(result.values, data.values)
	result.values = append(result.values, value{key: id, stored: stored, clone: clone})
	return result, nil
}

// Values returns every value stored under key, in insertion order.
func Values[T any](data Data, key metadata.Key[T]) []T {
	var result []T
	for _, entry := range data.values {
		if entry.key != key.ID() {
			continue
		}
		cloned, ok := entry.clone(entry.stored)
		if !ok {
			continue
		}
		typed, ok := cloned.(T)
		if !ok {
			continue
		}
		result = append(result, typed)
	}
	return result
}

// First returns the first value stored under key.
func First[T any](data Data, key metadata.Key[T]) (T, bool) {
	values := Values(data, key)
	if len(values) == 0 {
		var zero T
		return zero, false
	}
	return values[0], true
}
