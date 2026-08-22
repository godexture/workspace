package job

import (
	"sort"

	"github.com/godexture/godec/media/stream"
)

// Mapping is an exact inspected stream mapping between Job choices.
// Selector expressions and stream ordering policies are outside this M7
// contract.
type Mapping struct {
	input  int
	stream stream.ID
	output int
}

// MapStream selects one canonical stream from one input for one output.
func MapStream(input int, id stream.ID, output int) Mapping {
	return Mapping{input: input, stream: id, output: output}
}

func (m Mapping) Valid() bool       { return m.input >= 0 && !m.stream.IsZero() && m.output >= 0 }
func (m Mapping) Input() int        { return m.input }
func (m Mapping) Stream() stream.ID { return m.stream }
func (m Mapping) Output() int       { return m.output }

func cloneMappings(values []Mapping) []Mapping {
	result := append([]Mapping(nil), values...)
	sort.Slice(result, func(left, right int) bool {
		if result[left].input != result[right].input {
			return result[left].input < result[right].input
		}
		if result[left].output != result[right].output {
			return result[left].output < result[right].output
		}
		return result[left].stream.String() < result[right].stream.String()
	})
	return result
}
