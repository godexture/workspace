package metadata

import (
	"reflect"
	"slices"
	"testing"
)

type keyRaw map[any][]byte

type single interface {
	single()
}

type multiple interface {
	multiple()
}

type Bundle baseBundle

func NewBundle() *Bundle {
	return (*Bundle)(newBundle())
}

func Get[T single](b *Bundle) T {
	return get[T]((*baseBundle)(b))
}

func Enumerate[T multiple](b *Bundle) []T {
	return get[[]T]((*baseBundle)(b))
}

func (b *Bundle) Clear() {
	(*baseBundle)(b).clear()
}

func (b *Bundle) Set(value single) {
	(*baseBundle)(b).set(value)
}

func (b *Bundle) SetNonZero(value single) {
	(*baseBundle)(b).setNonZero(value)
}

func (b *Bundle) SetIfEmpty(value single) {
	(*baseBundle)(b).setIfEmpty(value)
}

func (b *Bundle) SetNonZeroIfEmpty(value single) {
	(*baseBundle)(b).setNonZeroIfEmpty(value)
}

func (b *Bundle) PushFront(value multiple) {
	tp := reflect.SliceOf(reflect.TypeOf(value))

	items := b.data[tp]
	if items == nil {
		items = reflect.MakeSlice(tp, 0, 0).Interface()
	}

	slice := reflect.MakeSlice(tp, 1, 1)
	slice.Index(0).Set(reflect.ValueOf(value))

	items = reflect.AppendSlice(slice, reflect.ValueOf(items)).Interface()
	b.data[tp] = items
}

func (b *Bundle) PushBack(value multiple) {
	tp := reflect.SliceOf(reflect.TypeOf(value))

	items := b.data[tp]
	if items == nil {
		items = reflect.MakeSlice(tp, 0, 0).Interface()
	}
	items = reflect.Append(reflect.ValueOf(items), reflect.ValueOf(value)).Interface()
	b.data[tp] = items
}

func (b *Bundle) SetRaw(key string, value []byte) {
	raw := Get[keyRaw](b)
	if raw == nil {
		raw = make(keyRaw)
	}

	raw[key] = value

	b.Set(raw)
}

func (b *Bundle) GetRaw(key string) ([]byte, bool) {
	raw := Get[keyRaw](b)
	if raw == nil {
		return nil, false
	}

	value, exists := raw[key]

	return value, exists
}

func AssertBundleValue[T interface {
	comparable
	single
}](t *testing.T, bundle *Bundle, expected T) {
	t.Helper()

	got := Get[T](bundle)

	if got != expected {
		t.Fatalf("Got (%v) = %v, expected %v", reflect.TypeFor[T]().Name(), got, expected)
	}
}

func AssertBundleSlice[T interface {
	comparable
	multiple
}](t *testing.T, bundle *Bundle, expected []T) {
	t.Helper()

	got := Enumerate[T](bundle)

	if !slices.Equal(got, expected) {
		t.Fatalf("Got (%v) = %v, expected %v", reflect.TypeFor[T](), got, expected)
	}
}
