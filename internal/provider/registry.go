package provider

import (
	"fmt"
	"sort"
	"sync"
)

// Registry is a thread-safe store of named providers.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry returns an empty, ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

// Register adds (or replaces) a provider keyed by its Name().
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// Get returns the provider registered under name, or an error if not found.
func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", name)
	}
	return p, nil
}

// List returns the sorted names of all registered providers.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
