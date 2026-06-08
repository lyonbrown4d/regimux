package admin_test

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func newAdminTestRuntimes(cfg config.Config) *collectionlist.List[ecosystem.Runtime] {
	return collectionlist.NewList[ecosystem.Runtime](
		ecosystem.NewConfigRuntime(ecosystem.Container, cfg.OrderedContainerUpstreams()),
		ecosystem.NewConfigRuntime(ecosystem.Go, cfg.OrderedGoUpstreams()),
		ecosystem.NewConfigRuntime(ecosystem.NPM, cfg.OrderedNPMUpstreams()),
		ecosystem.NewConfigRuntime(ecosystem.PyPI, cfg.OrderedPyPIUpstreams()),
		ecosystem.NewConfigRuntime(ecosystem.Maven, cfg.OrderedMavenUpstreams()),
	)
}
