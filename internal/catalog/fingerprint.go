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
	Kind       string
	Identity   string
	Plugin     string
	Descriptor plugin.Descriptor
	Schema     config.SchemaDescription
	Aliases    []string
	Provenance plugin.Provenance
}

type fingerprintDefinition struct {
	Kind       string
	Identity   string
	Descriptor plugin.Descriptor
}

func catalogFingerprint(definitions []plugin.Definition, components []plugin.Component) [32]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("godec/catalog/fingerprint/v1\x00"))
	sort.Slice(definitions, func(left, right int) bool {
		return definitions[left].Identity().String() < definitions[right].Identity().String()
	})
	for _, definition := range definitions {
		writeFingerprintEntry(hash, fingerprintDefinition{
			Kind:       "definition",
			Identity:   definition.Identity().String(),
			Descriptor: definition.Descriptor(),
		})
	}
	for _, component := range components {
		aliases := component.Aliases()
		sort.Strings(aliases)
		canonical := fingerprintComponent{
			Kind:       "component",
			Identity:   component.Identity().String(),
			Plugin:     component.PluginIdentity().String(),
			Descriptor: component.Descriptor(),
			Schema:     component.Schema().Description(),
			Aliases:    aliases,
			Provenance: component.Provenance(),
		}
		writeFingerprintEntry(hash, canonical)
	}
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func writeFingerprintEntry(hash interface{ Write([]byte) (int, error) }, entry any) {
	encoded, err := json.Marshal(entry)
	if err != nil {
		encoded = []byte("<invalid-catalog-fingerprint-entry>")
	}
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(encoded)))
	_, _ = hash.Write(length[:])
	_, _ = hash.Write(encoded)
}
