// Package ecosystems wires all supported dependency ecosystems.
package ecosystems

import (
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/golang"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/npm"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
)

var Module = dix.NewModule("ecosystems",
	dix.Imports(
		container.Module,
		dist.Module,
		golang.Module,
		npm.Module,
		pypi.Module,
		maven.Module,
	),
)
