package adapter

import (
	"fmt"
)

// Registry maintains mappings of provider types to AdapterFactories.
type Registry interface {
	Register(providerType string, factory AdapterFactory)
	Get(providerType string) (Adapter, error)
}

type registryImpl struct {
	factories map[string]AdapterFactory
}

// NewRegistry creates a new Registry instance.
func NewRegistry() Registry {
	return &registryImpl{
		factories: make(map[string]AdapterFactory),
	}
}

func (r *registryImpl) Register(providerType string, factory AdapterFactory) {
	r.factories[providerType] = factory
}

func (r *registryImpl) Get(providerType string) (Adapter, error) {
	factory, ok := r.factories[providerType]
	if !ok {
		return nil, fmt.Errorf("no adapter registered for provider type %q", providerType)
	}
	return factory(), nil
}
