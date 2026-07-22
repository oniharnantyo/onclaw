package tools

import (
	"context"
	"sync"
)

type ConfigEntry struct {
	JSONSchema string
	Load       func(ctx context.Context, cfg string) error
	Save       func(ctx context.Context) (string, error)
}

var (
	configRegistryMu sync.RWMutex
	configRegistry   = make(map[string]ConfigEntry)
)

func RegisterConfig(category string, jsonSchema string, load func(ctx context.Context, cfg string) error, save func(ctx context.Context) (string, error)) {
	configRegistryMu.Lock()
	defer configRegistryMu.Unlock()
	configRegistry[category] = ConfigEntry{
		JSONSchema: jsonSchema,
		Load:       load,
		Save:       save,
	}
}

func IsConfigurable(category string) bool {
	configRegistryMu.RLock()
	defer configRegistryMu.RUnlock()
	_, ok := configRegistry[category]
	return ok
}

func GetConfigEntry(category string) (ConfigEntry, bool) {
	configRegistryMu.RLock()
	defer configRegistryMu.RUnlock()
	entry, ok := configRegistry[category]
	return entry, ok
}
