package plugin

import (
	"strings"
	"unicode"

	"github.com/godexture/godec/diagnostic"
)

// Provenance records source/build information without becoming part of
// runtime identity.
type Provenance struct {
	Repository string
	Revision   string
	Builder    string
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
	PureGo      bool
	CGO         bool
	Native      bool
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
	return items
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
