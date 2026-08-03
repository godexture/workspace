package plugin

import (
	"strings"
	"unicode"

	"github.com/godexture/godec/diagnostic"
)

// Provenance records source/build information without becoming part of
// runtime identity.
type Provenance struct {
	Revision string
	Builder  string
}

// BuildMode identifies the implementation dependency mode. The zero value is
// unset and is intended for component descriptors that inherit from a plugin.
type BuildMode uint8

const (
	BuildModeUnset BuildMode = iota
	BuildModePureGo
	BuildModeCGO
	BuildModeNative
)

// String returns the stable surface spelling of a build mode.
func (m BuildMode) String() string {
	switch m {
	case BuildModeUnset:
		return "unset"
	case BuildModePureGo:
		return "pure-go"
	case BuildModeCGO:
		return "cgo"
	case BuildModeNative:
		return "native"
	default:
		return "unknown"
	}
}

// Descriptor contains display and distribution metadata. Only DisplayName
// and Version are required at M2; license and provenance checks are release
// policy handled by later milestones.
type Descriptor struct {
	DisplayName string
	Homepage    string
	Repository  string
	Version     string
	License     string
	Build       BuildMode
	Digest      string
	Signature   string
	Provenance  Provenance
}

// Validate returns descriptor diagnostics without mutating the descriptor.
func (d Descriptor) Validate() []diagnostic.Item {
	return d.diagnostics(diagnostic.Path{})
}

func (d Descriptor) diagnostics(path diagnostic.Path) []diagnostic.Item {
	var items []diagnostic.Item
	displayPath := path
	displayPath.Descriptor = "displayName"
	versionPath := path
	versionPath.Descriptor = "version"
	if strings.TrimSpace(d.DisplayName) == "" {
		items = append(items, diagnostic.NewItem("plugin.descriptor.display-name", diagnostic.ErrorSeverity, displayPath, "descriptor display name is required", nil))
	}
	if strings.TrimSpace(d.Version) == "" {
		items = append(items, diagnostic.NewItem("plugin.descriptor.version", diagnostic.ErrorSeverity, versionPath, "descriptor version is required", nil))
	}
	if d.Build > BuildModeNative {
		buildPath := path
		buildPath.Descriptor = "build"
		items = append(items, diagnostic.NewItem("plugin.descriptor.build-mode", diagnostic.ErrorSeverity, buildPath, "descriptor build mode is invalid", nil))
	}
	return items
}

func (d Descriptor) inherit(parent Descriptor) Descriptor {
	if d.Homepage == "" {
		d.Homepage = parent.Homepage
	}
	if d.Repository == "" {
		d.Repository = parent.Repository
	}
	if d.Version == "" {
		d.Version = parent.Version
	}
	if d.License == "" {
		d.License = parent.License
	}
	if d.Build == BuildModeUnset {
		d.Build = parent.Build
	}
	if d.Digest == "" {
		d.Digest = parent.Digest
	}
	if d.Signature == "" {
		d.Signature = parent.Signature
	}
	if d.Provenance.Revision == "" {
		d.Provenance.Revision = parent.Provenance.Revision
	}
	if d.Provenance.Builder == "" {
		d.Provenance.Builder = parent.Provenance.Builder
	}
	return d
}

func validAlias(alias string) bool {
	if alias == "" || strings.TrimSpace(alias) != alias {
		return false
	}
	for _, r := range alias {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '.' || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}
