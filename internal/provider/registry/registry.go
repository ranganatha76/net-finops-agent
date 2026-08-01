// Package registry is a thread-safe plugin map from cloud id → Factory.
// Any CSP registers a factory; the engine builds only enabled providers.
package registry

import (
	"fmt"
	"sync"

	"github.com/ranganatha76/net-finops-agent/internal/config"
	"github.com/ranganatha76/net-finops-agent/internal/provider"
)

// Factory builds a Scanner + Remediator pair for one CSP given config and mode ("demo"|"live").
type Factory func(cfg *config.Config, mode string) (provider.Scanner, provider.Remediator, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{} // cloud id → constructor
)

// Register adds (or replaces) a CSP factory. Safe to call from init().
func Register(cloud string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[cloud] = f
}

// Lookup returns a registered factory for cloud, if any.
func Lookup(cloud string) (Factory, bool) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := factories[cloud]
	return f, ok
}

// Registered returns all cloud ids that currently have factories.
func Registered() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(factories))
	for k := range factories {
		out = append(out, k)
	}
	return out
}

// BuildEnabled constructs scanners/remediators for every enabled provider in config
// that has a registered factory. Unknown enabled providers return an error.
func BuildEnabled(cfg *config.Config, mode string) ([]provider.Scanner, []provider.Remediator, error) {
	var scanners []provider.Scanner
	var remediators []provider.Remediator

	for _, id := range cfg.EnabledProviders() {
		f, ok := Lookup(id)
		if !ok {
			return nil, nil, fmt.Errorf(
				"provider %q is enabled but no factory is registered; known: %v — implement provider.Scanner/Remediator and call registry.Register(%q, ...)",
				id, Registered(), id,
			)
		}
		s, r, err := f(cfg, mode)
		if err != nil {
			return nil, nil, fmt.Errorf("provider %q: %w", id, err)
		}
		if s != nil {
			scanners = append(scanners, s)
		}
		if r != nil {
			remediators = append(remediators, r)
		}
	}
	return scanners, remediators, nil
}
