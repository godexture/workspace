package registry

import (
	"fmt"
)

func (r *Registry[V]) Register(manifest V) error {
	key, err := pluginKeyFromType(r.role, manifest.ConfigurationType())
	if err != nil {
		return fmt.Errorf("derive plugin key: %w", err)
	}
	manifest, err = assignManifestID(manifest, key)
	if err != nil {
		return err
	}

	if defaulter, ok := any(manifest).(Defaulter); ok {
		defaulter.ApplyDefaults()
	}

	if validator, ok := any(manifest).(Validator); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("invalid manifest: %w", err)
		}
	}

	r.registerMu.Lock()
	defer r.registerMu.Unlock()
	if _, exists := r.items.Load(key); exists {
		return fmt.Errorf("plugin already registered: %s", key)
	}
	if _, exists := r.names.Load(manifest.RegistryName()); exists {
		return fmt.Errorf("plugin name already registered: %s", manifest.RegistryName())
	}
	r.items.Store(key, manifest)
	r.names.Store(manifest.RegistryName(), key)

	return nil
}
