package catalog

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"sort"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/gotype"
	"github.com/godexture/godec/plugin"
)

type fingerprintComponent struct {
	Kind            string
	Identity        string
	Plugin          string
	Descriptor      plugin.Descriptor
	Schema          config.SchemaDescription
	Aliases         []string
	Provenance      plugin.Provenance
	Ports           []fingerprintPort
	HasSpec         bool
	HasSuggest      bool
	SuggestionLimit int
	Contract        plugin.Contract
	Traits          []plugin.TraitDescriptor
}

type fingerprintPort struct {
	ID           string
	Direction    flow.Direction
	Schema       string
	Payload      string
	HasTime      bool
	Required     bool
	Multiplicity flow.Multiplicity
	FanIn        flow.FanInPolicy
	Direct       bool
}

type fingerprintDefinition struct {
	Kind       string
	Identity   string
	Descriptor plugin.Descriptor
}

func catalogFingerprint(definitions []plugin.Definition, components []plugin.Component, declarations []plugin.Declaration) [32]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("godec/catalog/fingerprint/v6\x00"))
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
			Kind:            "component",
			Identity:        component.Identity().String(),
			Plugin:          component.PluginIdentity().String(),
			Descriptor:      component.Descriptor(),
			Schema:          component.Schema().Description(),
			Aliases:         aliases,
			Provenance:      component.Provenance(),
			Ports:           fingerprintPorts(component.Ports()),
			HasSpec:         component.View().HasSpec,
			HasSuggest:      component.View().HasSuggest,
			SuggestionLimit: component.View().SuggestionLimit,
			Contract:        component.Contract(),
			Traits:          component.Traits(),
		}
		writeFingerprintEntry(hash, canonical)
	}
	for _, declaration := range declarations {
		targets := declaration.Targets()
		targetNames := make([]string, len(targets))
		for index, target := range targets {
			targetNames[index] = target.String()
		}
		writeFingerprintEntry(hash, struct {
			Kind    string
			Key     string
			Targets []string
		}{Kind: "declaration", Key: declaration.Key().String(), Targets: targetNames})
	}
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func fingerprintPorts(shape flow.Shape) []fingerprintPort {
	ports := make([]fingerprintPort, 0, len(shape.Inputs)+len(shape.Outputs))
	for _, port := range append(append([]flow.Port(nil), shape.Inputs...), shape.Outputs...) {
		ports = append(ports, fingerprintPort{ID: port.ID(), Direction: port.Direction(), Schema: port.Schema().Identity().String(), Payload: gotype.Canonical(port.Schema().Payload()), HasTime: port.Schema().HasTime(), Required: port.Required(), Multiplicity: port.Multiplicity(), FanIn: port.FanIn(), Direct: port.Direct()})
	}
	sort.Slice(ports, func(left, right int) bool { return ports[left].ID < ports[right].ID })
	return ports
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
