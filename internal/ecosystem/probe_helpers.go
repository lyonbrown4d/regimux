package ecosystem

import (
	"net/http"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/samber/lo"
)

func probeEndpoints(cfg config.UpstreamConfig) *collectionlist.List[string] {
	endpoints := append([]string{cfg.Registry}, cfg.Mirrors...)
	normalized := lo.FilterMap(endpoints, func(endpoint string, _ int) (string, bool) {
		trimmed := strings.TrimRight(strings.TrimSpace(endpoint), "/")
		return trimmed, trimmed != ""
	})
	return collectionlist.NewList(normalized...)
}

func probeURL(endpoint string) string {
	return strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/"
}

func probeStatusReachable(status int) bool {
	return status >= http.StatusOK && status < http.StatusInternalServerError
}

func ScopedAlias(ecosystem, alias string) string {
	ecosystem = strings.TrimSpace(ecosystem)
	alias = strings.TrimSpace(alias)
	if ecosystem == "" || ecosystem == Container {
		return alias
	}
	return ecosystem + "/" + alias
}
