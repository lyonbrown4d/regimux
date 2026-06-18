package ecosystem

import (
	"net/http"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func probeEndpoints(cfg config.UpstreamConfig) *collectionlist.List[string] {
	out := collectionlist.NewList[string]()
	endpoints := append([]string{cfg.Registry}, cfg.Mirrors...)
	for _, endpoint := range endpoints {
		trimmed := strings.TrimRight(strings.TrimSpace(endpoint), "/")
		if trimmed != "" {
			out.Add(trimmed)
		}
	}
	return out
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
