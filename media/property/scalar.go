package property

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"unicode/utf8"
)

// scalar is a property value with a built-in canonical encoder. Named integer
// and string types cover ordinary Go enums.
type scalar interface {
	~bool |
		~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64 |
		~string
}

// Scalar returns the built-in encoder for a scalar property value.
func Scalar[T scalar]() Encoder[T] {
	typ := reflect.TypeFor[T]()
	return func(value T) ([]byte, error) {
		reflected := reflect.ValueOf(value)
		switch typ.Kind() {
		case reflect.Bool:
			if reflected.Bool() {
				return []byte("bool:1"), nil
			}
			return []byte("bool:0"), nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return []byte("int:" + strconv.FormatInt(reflected.Int(), 10)), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			return []byte("uint:" + strconv.FormatUint(reflected.Uint(), 10)), nil
		case reflect.Float32, reflect.Float64:
			value := reflected.Float()
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("property float must be finite")
			}
			if value == 0 {
				value = 0
			}
			bits := 64
			if typ.Kind() == reflect.Float32 {
				bits = 32
			}
			return []byte("float:" + strconv.FormatFloat(value, 'g', -1, bits)), nil
		case reflect.String:
			value := reflected.String()
			if !utf8.ValidString(value) {
				return nil, fmt.Errorf("property string must be valid UTF-8")
			}
			return []byte("string:" + value), nil
		}
		return nil, fmt.Errorf("property scalar encoder does not support %s", typ)
	}
}
