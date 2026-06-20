package optional

type Optional[T any] struct {
	value  T
	exists bool
}

func Some[T any](value T) Optional[T] {
	return Optional[T]{value: value, exists: true}
}

func None[T any]() Optional[T] {
	return Optional[T]{value: zeroValue[T](), exists: false}
}

func zeroValue[T any]() T {
	var zero T
	return zero
}

func (o Optional[T]) Exists() bool {
	return o.exists
}

func (o Optional[T]) ValueOr(value T) T {
	if !o.exists {
		return value
	}
	return o.value
}

func (o Optional[T]) Unwrap() T {
	if !o.exists {
		panic("attempted to unwrap None value")
	}

	return o.value
}
