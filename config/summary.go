package config

// SummaryField is one redacted, inert field projection suitable for a public
// Plan or DTO. Secret values are omitted rather than replaced with a string
// that could later be decoded as a real credential.
type SummaryField struct {
	ID       string
	Value    string
	Source   Source
	Redacted bool
}

// Summary describes one resolved config without retaining its typed value,
// codecs, closures, or secret material.
type Summary struct {
	schema      string
	version     string
	fingerprint Fingerprint
	fields      []SummaryField
}

func (s Summary) Valid() bool              { return s.schema != "" && s.version != "" && !s.fingerprint.IsZero() }
func (s Summary) Schema() string           { return s.schema }
func (s Summary) Version() string          { return s.version }
func (s Summary) Fingerprint() Fingerprint { return s.fingerprint }
func (s Summary) Fields() []SummaryField   { return append([]SummaryField(nil), s.fields...) }

func (s Summary) clone() Summary {
	s.fields = s.Fields()
	return s
}

func (s Schema[C]) summary(value C, provenance Provenance, fingerprint Fingerprint) Summary {
	fields := make([]SummaryField, 0, len(s.fields))
	for _, field := range s.fields {
		entry := SummaryField{ID: field.id, Redacted: field.description.Secret}
		entry.Source, _ = provenance.Source(field.id)
		if !entry.Redacted {
			fieldValue, err := field.read(&value)
			if err == nil {
				entry.Value = field.encode(fieldValue)
			}
		}
		fields = append(fields, entry)
	}
	return Summary{schema: s.identity, version: s.version, fingerprint: fingerprint, fields: fields}
}

func (s Schema[C]) patch(value C) (Patch, error) {
	result := NewPatch()
	for _, field := range s.fields {
		fieldValue, err := field.read(&value)
		if err != nil {
			return Patch{}, err
		}
		result = result.Set(field.key(s.identity), fieldValue)
	}
	if len(result.problems) != 0 {
		return Patch{}, diagnosticError(result.problems)
	}
	return result, nil
}
