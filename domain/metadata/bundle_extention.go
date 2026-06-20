package metadata

type Extensions baseBundle

func GetExt[T any](e *Extensions) T {
	b := (*baseBundle)(e)
	return get[T](b)
}

func (e *Extensions) Clear() {
	(*baseBundle)(e).clear()
}

func (e *Extensions) Set(value any) {
	(*baseBundle)(e).set(value)
}

func (e *Extensions) SetNonZero(value any) {
	(*baseBundle)(e).setNonZero(value)
}

func (e *Extensions) SetIfEmpty(value any) {
	(*baseBundle)(e).setIfEmpty(value)
}

func (e *Extensions) SetNonZeroIfEmpty(value any) {
	(*baseBundle)(e).setNonZeroIfEmpty(value)
}
