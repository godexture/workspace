package generator

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/godexture/tools/internal/config-generator/types"
)

// generateFieldOptions generates the With* functions for individual fields.
func generateFieldOptions(body *bytes.Buffer, fieldOrder []string, fieldsMap map[string]*types.FieldInfo) {
	for _, fieldName := range fieldOrder {
		info := fieldsMap[fieldName]

		if len(info.Targets) == 1 {
			t := info.Targets[0]
			optName := t.Type + "Option"
			funcOptName := strings.ToLower(optName[:1]) + optName[1:] + "Func"

			fmt.Fprintf(body, "func With%s(v %s) %s {\n", fieldName, info.TypeStr, optName)
			fmt.Fprintf(body, "\treturn %s(func(c *%s) {\n", funcOptName, t.Type)
			fmt.Fprintf(body, "\t\tc.%s = v\n", fieldName)
			fmt.Fprintf(body, "\t})\n}\n\n")
		} else {
			sharedOptIface := fieldName + "Option"
			fmt.Fprintf(body, "type %s interface {\n", sharedOptIface)
			for _, t := range info.Targets {
				fmt.Fprintf(body, "\t%sOption\n", t.Type)
			}
			fmt.Fprintf(body, "}\n\n")

			structName := strings.ToLower(fieldName[:1]) + fieldName[1:] + "Opt"
			fmt.Fprintf(body, "type %s struct { v %s }\n", structName, info.TypeStr)

			for _, t := range info.Targets {
				fmt.Fprintf(body, "func (o %s) apply%s(c *%s) {\n", structName, t.Type, t.Type)
				fmt.Fprintf(body, "\tc.%s = o.v\n", fieldName)
				fmt.Fprintf(body, "}\n")
			}
			fmt.Fprintf(body, "\n")

			fmt.Fprintf(body, "func With%s(v %s) %s {\n", fieldName, info.TypeStr, sharedOptIface)
			fmt.Fprintf(body, "\treturn %s{v}\n", structName)
			fmt.Fprintf(body, "}\n\n")
		}
	}
}

// generatePresetOptions generates the WithPreset function if any targets support presets.
func generatePresetOptions(body *bytes.Buffer, targets []*types.Target) {
	var presetTargets []*types.Target
	for _, t := range targets {
		if t.Preset != "" {
			presetTargets = append(presetTargets, t)
		}
	}

	if len(presetTargets) > 0 {
		if len(presetTargets) == 1 {
			t := presetTargets[0]
			optName := t.Type + "Option"
			funcOptName := strings.ToLower(optName[:1]) + optName[1:] + "Func"

			fmt.Fprintf(body, "func WithPreset(level int) %s {\n", optName)
			fmt.Fprintf(body, "\treturn %s(func(c *%s) {\n", funcOptName, t.Type)
			fmt.Fprintf(body, "\t\t*c = %s(%s(level))\n", t.Type, t.Preset)
			fmt.Fprintf(body, "\t})\n}\n\n")
			fmt.Fprintf(body, "func (c *%s) ApplyPreset(level int) {\n\t*c = New%s(WithPreset(level))\n}\n\n", t.Type, t.Type)
		} else {
			fmt.Fprintf(body, "type PresetOption interface {\n")
			for _, t := range presetTargets {
				fmt.Fprintf(body, "\t%sOption\n", t.Type)
			}
			fmt.Fprintf(body, "}\n\n")

			fmt.Fprintf(body, "type presetOpt int\n")
			for _, t := range presetTargets {
				fmt.Fprintf(body, "func (o presetOpt) apply%s(c *%s) {\n", t.Type, t.Type)
				fmt.Fprintf(body, "\t*c = %s(%s(int(o)))\n", t.Type, t.Preset)
				fmt.Fprintf(body, "}\n")
				fmt.Fprintf(body, "func (c *%s) ApplyPreset(level int) {\n\t*c = %s(%s(level))\n}\n", t.Type, t.Type, t.Preset)
			}
			fmt.Fprintf(body, "\nfunc WithPreset(level int) PresetOption {\n")
			fmt.Fprintf(body, "\treturn presetOpt(level)\n")
			fmt.Fprintf(body, "}\n\n")
		}
	}
}
