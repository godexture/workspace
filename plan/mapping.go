package plan

// Mapping is the inert projection of one resolved exact stream mapping.
// Sequence order is the resolved route ordinal; no separate ordinal is
// needed.
type Mapping struct {
	Input  int
	Stream string
	Output int
}

func (m Mapping) Valid() bool { return m.Input >= 0 && m.Stream != "" && m.Output >= 0 }
