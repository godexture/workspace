package metadata

import (
	"reflect"
	"testing"
)

type keyRaw map[string][][]byte

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

func (b *Bundle) AddRaw(key string, value []byte) {
	raw := Get[keyRaw](b)
	if raw == nil {
		raw = make(keyRaw)
	}

	raw[key] = append(raw[key], value)

	b.Set(raw)
}

func (b *Bundle) GetRaw(key string) ([][]byte, bool) {
	raw := Get[keyRaw](b)
	if raw == nil {
		return nil, false
	}

	value, exists := raw[key]

	return value, exists
}

func (b *Bundle) AllRaw() map[string][][]byte {
	return Get[keyRaw](b)
}

func AssertBundleValue[T interface {
	comparable
	single
}](t *testing.T, bundle *Bundle, expected T) {
	t.Helper()

	got := Get[T](bundle)

	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("Got (%v) = %v, expected %v", reflect.TypeFor[T]().Name(), got, expected)
	}
}

func AssertBundleSlice[T interface {
	comparable
	multiple
}](t *testing.T, bundle *Bundle, expected []T) {
	t.Helper()

	got := Enumerate[T](bundle)

	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("Got (%v) = %v, expected %v", reflect.TypeFor[T](), got, expected)
	}
}

func (b *Bundle) Merge(other *Bundle) {
	if other == nil {
		return
	}
	for tp, val := range other.data {
		if tp == reflect.TypeFor[keyRaw]() {
			destRaw := b.data[tp]
			if destRaw == nil {
				destRaw = make(keyRaw)
				b.data[tp] = destRaw
			}
			rawMap := destRaw.(keyRaw)
			srcRaw := val.(keyRaw)
			for k, v := range srcRaw {
				rawMap[k] = append(rawMap[k], v...)
			}
			continue
		}

		if tp.Kind() == reflect.Slice {
			destSliceVal := reflect.ValueOf(b.data[tp])
			if !destSliceVal.IsValid() {
				srcSliceVal := reflect.ValueOf(val)
				newSlice := reflect.MakeSlice(tp, srcSliceVal.Len(), srcSliceVal.Len())
				reflect.Copy(newSlice, srcSliceVal)
				b.data[tp] = newSlice.Interface()
				continue
			}

			srcSliceVal := reflect.ValueOf(val)
			for i := 0; i < srcSliceVal.Len(); i++ {
				elem := srcSliceVal.Index(i)
				found := false
				for j := 0; j < destSliceVal.Len(); j++ {
					if reflect.DeepEqual(destSliceVal.Index(j).Interface(), elem.Interface()) {
						found = true
						break
					}
				}
				if !found {
					destSliceVal = reflect.Append(destSliceVal, elem)
				}
			}
			b.data[tp] = destSliceVal.Interface()
			continue
		}

		(*baseBundle)(b).setNonZero(val)
	}
}
