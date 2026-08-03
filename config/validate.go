package config

import (
	"reflect"
	"strings"

	"github.com/godexture/godec/diagnostic"
)

func (s Schema[C]) validateDefinition() []diagnostic.Item {
	var items []diagnostic.Item
	var defaultValue C
	defaultReady := false
	if strings.TrimSpace(s.identity) == "" {
		items = append(items, diagnostic.NewItem(codeMissingIdentity, diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: "identity"}, "schema identity is required", nil))
	}
	if strings.TrimSpace(s.version) == "" {
		items = append(items, diagnostic.NewItem(codeMissingVersion, diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: "version"}, "schema version is required", nil))
	}
	if s.defaults == nil {
		items = append(items, diagnostic.NewItem(codeDefaultFactory, diagnostic.ErrorSeverity, diagnostic.Path{}, "schema default factory is required", nil))
	}
	fieldIDs := make(map[string]struct{}, len(s.fields))
	for _, field := range s.fields {
		fieldIDs[field.id] = struct{}{}
	}
	aliases := make(map[string]string)
	for _, field := range s.fields {
		path := diagnostic.FieldPath(field.id)
		if field.id == "" || strings.TrimSpace(field.id) != field.id {
			items = append(items, diagnostic.NewItem(codeInvalidSchema, diagnostic.ErrorSeverity, path, "field ID must be non-empty and trimmed", nil))
		}
		if countFieldIDs(s.fields, field.id) > 1 {
			items = append(items, diagnostic.NewItem(codeDuplicateField, diagnostic.ErrorSeverity, path, "field IDs must be unique", nil))
		}
		for _, problem := range field.construction {
			items = append(items, prefixItem(problem, field.id))
		}
		seenAliases := make(map[string]struct{}, len(field.aliases))
		for _, alias := range field.aliases {
			if alias == "" || strings.TrimSpace(alias) != alias || strings.IndexFunc(alias, func(r rune) bool { return r == '.' || r == '\\' || r == '/' }) >= 0 {
				items = append(items, diagnostic.NewItem(codeInvalidAlias, diagnostic.ErrorSeverity, path, "field alias must be a trimmed search name without path separators", nil))
			}
			if _, exists := seenAliases[alias]; exists {
				items = append(items, diagnostic.NewItem(codeInvalidAlias, diagnostic.ErrorSeverity, path, "field aliases must be unique within a field", nil))
			}
			seenAliases[alias] = struct{}{}
			if previous, exists := aliases[alias]; exists && previous != field.id {
				// Aliases may be shared across components, but not within one schema.
				items = append(items, diagnostic.NewItem(codeInvalidAlias, diagnostic.ErrorSeverity, path, "field aliases must identify one field within a schema", nil))
			}
			aliases[alias] = field.id
		}
		for _, dependency := range field.dependsOn {
			if _, exists := fieldIDs[dependency]; !exists {
				items = append(items, diagnostic.NewItem(codeFieldDependency, diagnostic.ErrorSeverity, path, "field depends on an unknown field", map[string]string{"dependency": dependency}))
			}
		}
	}
	for left, leftField := range s.fields {
		for right := left + 1; right < len(s.fields); right++ {
			if pathCollision(leftField.id, s.fields[right].id) {
				items = append(items, diagnostic.NewItem(codeFieldPathCollision, diagnostic.ErrorSeverity, diagnostic.FieldPath(leftField.id), "nested field paths collide", map[string]string{"other": s.fields[right].id}))
			}
		}
	}
	if hasDependencyCycle(s.fields) {
		items = append(items, diagnostic.NewItem(codeFieldDependency, diagnostic.ErrorSeverity, diagnostic.Path{}, "field dependencies contain a cycle", nil))
	}
	if s.defaults != nil {
		value, factoryItems := s.defaultValue()
		items = append(items, factoryItems...)
		items = append(items, s.validateUnregisteredFields(value)...)
		defaultValue = value
		defaultReady = len(factoryItems) == 0
	}
	presetIDs := make(map[string]struct{}, len(s.presets))
	for _, preset := range s.presets {
		path := diagnostic.Path{Fields: []string{"preset", preset.id}}
		if preset.id == "" || strings.TrimSpace(preset.id) != preset.id {
			items = append(items, diagnostic.NewItem(codeInvalidSchema, diagnostic.ErrorSeverity, path, "preset ID must be non-empty and trimmed", nil))
		}
		if _, exists := presetIDs[preset.id]; exists {
			items = append(items, diagnostic.NewItem(codeDuplicatePreset, diagnostic.ErrorSeverity, path, "preset IDs must be unique", nil))
		}
		presetIDs[preset.id] = struct{}{}
		if preset.apply == nil {
			items = append(items, diagnostic.NewItem(codePresetFactory, diagnostic.ErrorSeverity, path, "preset apply function is required", nil))
		}
	}
	if defaultReady {
		value := defaultValue
		items = append(items, s.validateValue(value)...)
		_, canonicalItems := s.canonicalValue(value)
		items = append(items, canonicalItems...)
		for _, preset := range s.presets {
			if preset.apply == nil {
				continue
			}
			presetValue, snapshotItems := s.snapshot(value)
			items = append(items, snapshotItems...)
			if err := callPreset(preset.apply, &presetValue); err != nil {
				items = append(items, diagnostic.NewItem(codePresetFactory, diagnostic.ErrorSeverity, diagnostic.Path{Fields: []string{"preset", preset.id}}, "preset application failed", nil))
				continue
			}
			for _, item := range s.validateValue(presetValue) {
				items = append(items, prefixPresetItem(item, preset.id))
			}
			_, presetCanonicalItems := s.canonicalValue(presetValue)
			for _, item := range presetCanonicalItems {
				items = append(items, prefixPresetItem(item, preset.id))
			}
		}
	}
	return items
}

// Every top-level field must be registered so its codec defines the value and
// snapshot boundaries.
func (s Schema[C]) validateUnregisteredFields(value C) []diagnostic.Item {
	typ := reflect.TypeFor[C]()
	root := reflect.ValueOf(&value).Elem()
	registered := make(map[uintptr][]reflect.Type, len(s.fields))
	for _, field := range s.fields {
		if field.target == nil {
			continue
		}
		if pointer := field.target(&value); pointer != 0 {
			registered[pointer] = append(registered[pointer], field.valueType)
		}
	}
	if typ.Kind() != reflect.Struct {
		pointer, ok := fieldAddress(root)
		if ok && registeredField(registered[pointer], typ) {
			return nil
		}
		return []diagnostic.Item{diagnostic.NewItem(codeUnregisteredField, diagnostic.ErrorSeverity, diagnostic.FieldPath("<root>"), "config value is not registered through a field codec", map[string]string{"type": typ.String()})}
	}

	var items []diagnostic.Item
	for index := 0; index < typ.NumField(); index++ {
		fieldType := typ.Field(index)
		if fieldType.Name == "_" || fieldType.Type.Size() == 0 {
			continue
		}
		fieldValue := root.Field(index)
		pointer, ok := fieldAddress(fieldValue)
		if !ok {
			items = append(items, diagnostic.NewItem(codeUnregisteredField, diagnostic.ErrorSeverity, diagnostic.FieldPath(fieldType.Name), "config field cannot be addressed by the schema", map[string]string{"type": fieldType.Type.String()}))
			continue
		}
		if registeredField(registered[pointer], fieldType.Type) {
			continue
		}
		items = append(items, diagnostic.NewItem(codeUnregisteredField, diagnostic.ErrorSeverity, diagnostic.FieldPath(fieldType.Name), "config field is not registered by the schema", map[string]string{"type": fieldType.Type.String()}))
	}
	return items
}

func registeredField(types []reflect.Type, fieldType reflect.Type) bool {
	for _, registeredType := range types {
		if registeredType == fieldType {
			return true
		}
	}
	return false
}

func fieldAddress(value reflect.Value) (pointer uintptr, ok bool) {
	if !value.IsValid() || !value.CanAddr() {
		return 0, false
	}
	defer func() {
		if recover() != nil {
			pointer = 0
			ok = false
		}
	}()
	return value.Addr().Pointer(), true
}

func countFieldIDs[C any](fields []FieldSpec[C], id string) int {
	count := 0
	for _, field := range fields {
		if field.id == id {
			count++
		}
	}
	return count
}

func prefixItem(item diagnostic.Item, field string) diagnostic.Item {
	if item.Path.IsZero() {
		item.Path = diagnostic.FieldPath(field)
	} else {
		item.Path = item.Path.Prefix(diagnostic.FieldPath(field))
	}
	return item
}

func prefixPresetItem(item diagnostic.Item, preset string) diagnostic.Item {
	item.Path = item.Path.Prefix(diagnostic.Path{Fields: []string{"preset", preset}})
	return item
}

func pathCollision(left, right string) bool {
	return left != right && (strings.HasPrefix(left, right+".") || strings.HasPrefix(right, left+"."))
}

func hasDependencyCycle[C any](fields []FieldSpec[C]) bool {
	dependencies := make(map[string][]string, len(fields))
	for _, field := range fields {
		dependencies[field.id] = append([]string(nil), field.dependsOn...)
	}
	state := make(map[string]uint8, len(fields))
	var visit func(string) bool
	visit = func(id string) bool {
		switch state[id] {
		case 1:
			return true
		case 2:
			return false
		}
		state[id] = 1
		for _, dependency := range dependencies[id] {
			if visit(dependency) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for id := range dependencies {
		if visit(id) {
			return true
		}
	}
	return false
}
