package container

import (
	"fmt"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
)

func (e *RegistryEndpoint) routeFromInput(input *registryInput) (reference.Route, error) {
	if e == nil || input == nil || e.defaultContainerAlias == "" {
		return routeFromInput(input)
	}
	route, err := reference.ParseWithDefaultAlias(
		input.path(),
		e.defaultContainerAlias,
		e.hasContainerAlias,
	)
	if err != nil {
		return reference.Route{}, fmt.Errorf("parse container route with default alias: %w", err)
	}
	return route, nil
}

func (e *RegistryEndpoint) hasContainerAlias(alias string) bool {
	if e == nil {
		return false
	}
	_, ok := e.containerAliases[alias]
	return ok
}
