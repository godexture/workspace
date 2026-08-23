package g711

import tables "github.com/godexture/godec/plugin/pcm/internal/g711"

// Law is one of the two G.711 companding curves. It belongs to the component
// rather than to its configuration, because a composition binds a container
// tag to a component and cannot carry configuration with it.
type Law uint8

const (
	ALaw Law = iota + 1
	ULaw
)

func (l Law) Valid() bool { return l == ALaw || l == ULaw }

func (l Law) String() string {
	switch l {
	case ALaw:
		return "alaw"
	case ULaw:
		return "ulaw"
	default:
		return "unknown companding law"
	}
}

func (l Law) expansion() *[256]uint16 {
	if l == ALaw {
		return tables.ALawExpansion()
	}
	return tables.ULawExpansion()
}

func (l Law) companding() *[65536]byte {
	if l == ALaw {
		return tables.ALawCompanding()
	}
	return tables.ULawCompanding()
}
