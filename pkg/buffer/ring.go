package buffer

// Ring is a FIFO buffer that keeps its readable data contiguous.
//
// Slices returned by Data, Grow, and Take remain valid until the next call
// that grows, truncates, resets, or releases the buffer. Discard and Take do
// not overwrite returned data, but a later mutation may compact the buffer
// and reuse its storage.
//
// The zero value is ready for use. Ring is not safe for concurrent use.
type Ring[T any] struct {
	data []T
	head int
}

// NewRing creates an empty Ring with space for at least capacity elements.
func NewRing[T any](capacity int) Ring[T] {
	return Ring[T]{data: make([]T, 0, capacity)}
}

// Len returns the number of readable elements.
func (r *Ring[T]) Len() int {
	return len(r.data) - r.head
}

// Cap returns the capacity of the backing storage.
func (r *Ring[T]) Cap() int {
	return cap(r.data)
}

// Data returns the readable elements as one contiguous slice.
func (r *Ring[T]) Data() []T {
	return r.data[r.head:]
}

// Append adds values to the end of the buffer.
func (r *Ring[T]) Append(values []T) {
	if len(values) == 0 {
		return
	}
	copy(r.Grow(len(values)), values)
}

// Grow extends the buffer by n elements and returns the appended region.
// Existing values in the returned region are unspecified and must be
// initialized by the caller before they are read.
func (r *Ring[T]) Grow(n int) []T {
	if n < 0 {
		panic("buffer: negative Ring.Grow")
	}
	r.ensureTail(n)
	start := len(r.data)
	r.data = r.data[:start+n]
	return r.data[start:]
}

// Truncate keeps the first n readable elements.
func (r *Ring[T]) Truncate(n int) {
	if n < 0 || n > r.Len() {
		panic("buffer: Ring.Truncate out of range")
	}
	if n == 0 {
		r.Reset()
		return
	}
	r.data = r.data[:r.head+n]
}

// Take removes and returns the first n readable elements.
func (r *Ring[T]) Take(n int) []T {
	if n < 0 || n > r.Len() {
		panic("buffer: Ring.Take out of range")
	}
	if n == 0 {
		return nil
	}
	out := r.data[r.head : r.head+n]
	r.head += n
	if r.head == len(r.data) {
		r.data = r.data[:0]
		r.head = 0
	}
	return out
}

// Discard removes the first n readable elements.
func (r *Ring[T]) Discard(n int) {
	_ = r.Take(n)
}

// Reset removes all elements while retaining the backing storage.
func (r *Ring[T]) Reset() {
	r.data = r.data[:0]
	r.head = 0
}

// TakeAll returns all readable elements and releases the backing storage.
func (r *Ring[T]) TakeAll() []T {
	if r.Len() == 0 {
		r.data = nil
		r.head = 0
		return nil
	}
	out := r.Data()
	r.data = nil
	r.head = 0
	return out
}

func (r *Ring[T]) ensureTail(n int) {
	if cap(r.data)-len(r.data) >= n {
		return
	}

	live := r.Len()
	if r.head >= (live+1)/2 {
		copy(r.data[:live], r.data[r.head:])
		r.data = r.data[:live]
		r.head = 0
		if cap(r.data)-len(r.data) >= n {
			return
		}
	}

	newCapacity := live + n
	if doubled := 2 * cap(r.data); doubled > newCapacity {
		newCapacity = doubled
	}
	grown := make([]T, live, newCapacity)
	copy(grown, r.Data())
	r.data = grown
	r.head = 0
}
