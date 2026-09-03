package plan

import "github.com/godexture/godec/media/stream"

// Descriptor is an inert stream-state projection. It deliberately does not
// retain schema factories, metadata values, or property values.
type Descriptor struct {
	Stream               string
	Schema               string
	HasTimeline          bool
	TimeBaseNumerator    int64
	TimeBaseDenominator  int64
	PropertyFingerprint  string
	MetadataAvailability string
	MetadataScope        string
	Fingerprint          string
}

func (d Descriptor) Valid() bool {
	timeBaseValid := d.HasTimeline && d.TimeBaseNumerator > 0 && d.TimeBaseDenominator > 0 ||
		!d.HasTimeline && d.TimeBaseNumerator == 0 && d.TimeBaseDenominator == 0
	metadataValid := false
	switch d.MetadataAvailability {
	case "absent":
		metadataValid = d.MetadataScope == "unknown"
	case "available", "unavailable":
		metadataValid = validMetadataScope(d.MetadataScope)
	}
	return d.Stream != "" && d.Schema != "" && timeBaseValid && d.PropertyFingerprint != "" && metadataValid && d.Fingerprint != ""
}

func validMetadataScope(value string) bool {
	switch value {
	case "asset", "program", "stream", "chapter":
		return true
	default:
		return false
	}
}

// ProjectDescriptor removes runtime factories and values from a descriptor.
func ProjectDescriptor(value stream.Descriptor) (Descriptor, error) {
	fingerprint, err := value.Fingerprint()
	if err != nil {
		return Descriptor{}, err
	}
	timeBase := value.TimeBase()
	return Descriptor{
		Stream:               value.ID().String(),
		Schema:               value.Schema().String(),
		HasTimeline:          value.HasTimeline(),
		TimeBaseNumerator:    timeBase.Numerator,
		TimeBaseDenominator:  timeBase.Denominator,
		PropertyFingerprint:  value.Properties().Fingerprint().String(),
		MetadataAvailability: value.Metadata().State().String(),
		MetadataScope:        value.Metadata().Scope().String(),
		Fingerprint:          fingerprint.String(),
	}, nil
}

type PortDescriptor struct {
	Port       string
	Descriptor Descriptor
}

func (d PortDescriptor) Valid() bool { return d.Port != "" && d.Descriptor.Valid() }
