package metadata

import "reflect"

type Bundle struct {
	data map[any]any
}

func NewBundle() *Bundle {
	return &Bundle{
		data: make(map[any]any),
	}
}

func (b *Bundle) Set(key any, value any) {
	b.data[key] = value
}

func (b *Bundle) Clear() {
	clear(b.data)
}

func Get[T any](b *Bundle, key any) (T, error) {
	var zero T

	if b == nil || b.data == nil {
		return zero, ErrNotFound
	}

	val, exists := b.data[key]
	if !exists {
		return zero, ErrNotFound
	}

	typedVal, ok := val.(T)
	if !ok {
		return zero, &TypeError{
			Key:      key,
			Expected: reflect.TypeOf(zero).String(),
			Actual:   reflect.TypeOf(val).String(),
		}
	}

	return typedVal, nil
}
