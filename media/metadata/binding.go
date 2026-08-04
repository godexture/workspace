package metadata

import (
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin"
)

// Binding connects a carrier slot to the metadata encoding that interprets its
// payload. It is the metadata counterpart of a codec binding and rides the same
// composition declaration, so host construction rejects one carrier bound to
// two different encodings without an explicit override.
type Binding = plugin.Declaration

type bindingNamespace struct{}

// Bind declares that payloads found in carrier are parsed and marshalled by the
// encoding component. Neither the format that owns the carrier nor the encoding
// imports the other; composition is what joins them.
func Bind(carrier format.CarrierID, encoding plugin.Identity) Binding {
	return plugin.Declare[bindingNamespace](string(carrier), encoding)
}

// BindingKey returns the declaration key a carrier binds under, for callers
// that need to override an existing binding.
func BindingKey(carrier format.CarrierID) plugin.DeclarationKey {
	return Bind(carrier, plugin.Identity{}).Key()
}
