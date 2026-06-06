// Package container wires the OCI / Docker Registry ecosystem runtime.
package container

import (
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	"github.com/lyonbrown4d/regimux/internal/upstream"
)

var Module = dix.NewModule("container-ecosystem",
	dix.Providers(
		dix.Provider3[*Runtime, config.Config, *upstream.Client, *prefetch.Service](
			NewRuntime,
			dix.Into[ecosystem.Runtime](dix.Key("container"), dix.Order(0)),
		),
	),
)
