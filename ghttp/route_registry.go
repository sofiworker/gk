package ghttp

import (
	"fmt"
	"sync"
)

type routeRegistry struct {
	mu sync.Mutex

	definitions []routeDefinition
	byMethod    map[string]map[string]struct{}
	paramNames  map[string]string
	reserved    map[string]struct{}
}

func newRouteRegistry(_ bool) *routeRegistry {
	return &routeRegistry{
		byMethod:   make(map[string]map[string]struct{}),
		paramNames: make(map[string]string),
		reserved:   make(map[string]struct{}),
	}
}

func (r *routeRegistry) reserve(pattern routePattern) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reserved[pattern.path] = struct{}{}
}

func (r *routeRegistry) register(definitions ...routeDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	byMethod := cloneRouteMethodIndex(r.byMethod)
	paramNames := cloneRouteParamNames(r.paramNames)
	for _, definition := range definitions {
		if !definition.internal {
			if _, reserved := r.reserved[definition.pattern.path]; reserved {
				return fmt.Errorf("%w: %s %s conflicts with an internal endpoint", ErrRouteConflict, definition.method, definition.pattern.path)
			}
		}
		if err := validateRouteDefinition(definition, byMethod, paramNames); err != nil {
			return err
		}
	}

	for _, definition := range definitions {
		r.definitions = append(r.definitions, definition.clone())
	}
	r.byMethod = byMethod
	r.paramNames = paramNames
	return nil
}

func (r *routeRegistry) snapshot() []routeDefinition {
	r.mu.Lock()
	defer r.mu.Unlock()

	definitions := make([]routeDefinition, len(r.definitions))
	for i, definition := range r.definitions {
		definitions[i] = definition.clone()
	}
	return definitions
}

func validateRouteDefinition(definition routeDefinition, byMethod map[string]map[string]struct{}, paramNames map[string]string) error {
	structureKey := definition.pattern.structureKey()
	methodRoutes := byMethod[definition.method]
	if methodRoutes == nil {
		methodRoutes = make(map[string]struct{})
		byMethod[definition.method] = methodRoutes
	}
	if _, exists := methodRoutes[structureKey]; exists {
		return fmt.Errorf("%w: %s %s", ErrRouteConflict, definition.method, definition.pattern.path)
	}
	for _, parameter := range definition.pattern.dynamicParameterKeys() {
		if existingName, exists := paramNames[parameter.key]; exists && existingName != parameter.name {
			return fmt.Errorf("%w: parameter %q conflicts with %q in %s %s", ErrRouteConflict, parameter.name, existingName, definition.method, definition.pattern.path)
		}
		paramNames[parameter.key] = parameter.name
	}
	methodRoutes[structureKey] = struct{}{}
	return nil
}

func cloneRouteMethodIndex(src map[string]map[string]struct{}) map[string]map[string]struct{} {
	cloned := make(map[string]map[string]struct{}, len(src))
	for method, paths := range src {
		clonedPaths := make(map[string]struct{}, len(paths))
		for path := range paths {
			clonedPaths[path] = struct{}{}
		}
		cloned[method] = clonedPaths
	}
	return cloned
}

func cloneRouteParamNames(src map[string]string) map[string]string {
	cloned := make(map[string]string, len(src))
	for key, name := range src {
		cloned[key] = name
	}
	return cloned
}
