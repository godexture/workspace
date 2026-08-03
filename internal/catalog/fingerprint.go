package catalog

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"sort"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/plugin"
)

type fingerprintComponent struct {
	Identity   string
	Plugin     string
	Descriptor plugin.Descriptor
	Schema     config.SchemaDescription
	Aliases    []string
	Provenance plugin.Provenance
}

func catalogFingerprint(components []plugin.Component) [32]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("godec/catalog/fingerprint/v1\x00"))
	for _, component := range components {
		aliases := component.Aliases()
		sort.Strings(aliases)
		canonical := fingerprintComponent{
			Identity:   component.Identity().String(),
			Plugin:     component.PluginIdentity().String(),
			Descriptor: component.Descriptor(),
			Schema:     component.Schema().Description(),
			Aliases:    aliases,
			Provenance: component.Provenance(),
		}
		encoded, err := json.Marshal(canonical)
		if err != nil {
			encoded = []byte("<invalid-catalog-fingerprint-entry>")
		}
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(encoded)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write(encoded)
	}
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result
}
