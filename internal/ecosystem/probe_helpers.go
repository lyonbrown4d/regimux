package ecosystem

import (
	"net/http"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/samber/lo"
)

func probeEndpoints(cfg config.UpstreamConfig) *collectionlist.List[string] {
	return collectionlist.NewList(lo.FilterMap(lo.Concat([]string{cfg.Registry}, cfg.Mirrors), func(endpoint string, _ int) (string, bool) {
		trimmed := strings.TrimRight(strings.TrimSpace(endpoint), "/")
		return trimmed, trimmed != ""
	})...)
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

func applyProbeAuth(req *http.Request, cfg config.AuthConfig) {
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "basic":
		req.SetBasicAuth(cfg.Username, cfg.Password)
	case "bearer":
		if token := strings.TrimSpace(cfg.Token); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
}
