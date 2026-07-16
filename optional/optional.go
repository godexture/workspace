package optional

type Optional[T any] struct {
	value *T
}

func Some[T any](value T) Optional[T] {
	return Optional[T]{value: &value}
}

func None[T any]() Optional[T] {
	return Optional[T]{value: nil}
}

func (o Optional[T]) Exists() bool {
	return o.value != nil
}

func (o Optional[T]) ValueOr(value T) T {
	if o.value == nil {
		return value
	}
	return *o.value
}

func (o Optional[T]) Unwrap() T {
	if o.value == nil {
		panic("attempted to unwrap None value")
	}

	return *o.value
}
