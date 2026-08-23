package codec

import (
	"bytes"
	"fmt"

	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/property"
)

// Parameters is the codec-defined configuration a container carries verbatim
// beside a stream: the coefficient table of an ADPCM block, the decoder setup
// of an elementary stream, whatever the codec named by Tag needs and its
// container has no field for.
//
// A container never interprets these bytes. What they mean is fixed by the
// codec, which is what lets a container that has never heard of that codec
// still store them and hand them back unchanged.
//
// Copying Parameters shares the backing array, so passing them along a graph
// never copies the payload. The payload is copied once on the way in and is
// never handed out as a mutable slice, which is what makes the sharing safe.
type Parameters struct{ state *parameterState }

type parameterState struct{ data []byte }

// NewParameters copies data into an immutable payload. This is the only copy:
// every later Parameters value shares this backing.
func NewParameters(data []byte) Parameters {
	if len(data) == 0 {
		return Parameters{}
	}
	return Parameters{state: &parameterState{data: append([]byte(nil), data...)}}
}

func (p Parameters) Valid() bool { return p.state != nil }

func (p Parameters) Len() int {
	if p.state == nil {
		return 0
	}
	return len(p.state.data)
}

// AppendTo copies the payload only when a caller actually needs the bytes.
func (p Parameters) AppendTo(destination []byte) []byte {
	if p.state == nil {
		return destination
	}
	return append(destination, p.state.data...)
}

// Equal reports payload equality. Parameters from different sources compare by
// content, so a rewritten header can be checked against the one it came from.
func (p Parameters) Equal(other Parameters) bool {
	if p.state == nil || other.state == nil {
		return p.state == nil && other.state == nil
	}
	return bytes.Equal(p.state.data, other.state.data)
}

type parameterKeyID struct{}

// Parameters participate in the descriptor fingerprint: a stream whose codec
// is configured differently is a different stream, even when everything the
// container states about it is the same.
var parameterProperty = property.Define[parameterKeyID](func(value Parameters) ([]byte, error) {
	if !value.Valid() {
		return nil, fmt.Errorf("codec parameters are empty")
	}
	return value.AppendTo([]byte("codec-parameters:")), nil
}, func(value Parameters) Parameters { return value })

func WithParameters(properties property.Set, value Parameters) (property.Set, error) {
	return parameterProperty.Set(properties, value)
}

func ParametersOf(properties property.Set) (Parameters, bool) {
	value, ok := parameterProperty.Get(properties)
	return value, ok && value.Valid()
}

// WithoutParameters drops the codec configuration, which is what a decoded
// stream does: it is no longer the coded one those bytes configured.
func WithoutParameters(properties property.Set) property.Set {
	return properties.Delete(parameterKey())
}

func parameterKey() key.ID { return parameterProperty.ID() }
