package metadata

import (
	"reflect"
)

type baseBundle struct {
	data map[reflect.Type]any
}

func newBundle() *baseBundle {
	return &baseBundle{
		data: make(map[reflect.Type]any),
	}
}

func get[T any](b *baseBundle) T {
	tp := reflect.TypeFor[T]()
	if tp.Kind() == reflect.Ptr {
		panic("pointers are not allowed")
	}

	var zero T

	value, exists := b.data[tp]
	if !exists {
		return zero
	}

	typed, _ := value.(T)
	return typed
}

func (b *baseBundle) clear() {
	b.data = make(map[reflect.Type]any)
}

func (b *baseBundle) set(value any) {
	tp := reflect.TypeOf(value)
	if tp.Kind() == reflect.Ptr {
		panic("pointers are not allowed")
	}

	b.data[tp] = value
}

func (b *baseBundle) setNonZero(value any) {
	if !reflect.ValueOf(value).IsZero() {
		b.set(value)
	}
}

func (b *baseBundle) setIfEmpty(value any) {
	if _, exists := b.data[reflect.TypeOf(value)]; !exists {
		b.set(value)
	}
}

func (b *baseBundle) setNonZeroIfEmpty(value any) {
	if !reflect.ValueOf(value).IsZero() {
		b.setIfEmpty(value)
	}
}
