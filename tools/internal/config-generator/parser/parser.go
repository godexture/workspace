package parser

import (
	"strings"

	"github.com/godexture/tools/internal/config-generator/types"
)

// ParseTarget parses a target definition string into a Target struct.
func ParseTarget(s string) *types.Target {
	parts := strings.Split(s, ",")
	t := &types.Target{Type: parts[0], ExtraImports: make(map[string]string)}
	for _, part := range parts[1:] {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			switch kv[0] {
			case "default":
				t.Default = kv[1]
			case "preset":
				t.Preset = kv[1]
			case "source":
				t.Source = kv[1]
			case "resolved-type":
				t.ResolvedType = kv[1]
			case "import":
				ikv := strings.SplitN(kv[1], "=", 2)
				if len(ikv) == 2 {
					t.ExtraImports[ikv[0]] = ikv[1]
				}
			}
		}
	}
	return t
}
