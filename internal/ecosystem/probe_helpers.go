package ecosystem

import (
	"net/http"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func probeEndpoints(cfg config.UpstreamConfig) *collectionlist.List[string] {
	endpoints := collectionlist.NewList(append([]string{cfg.Registry}, cfg.Mirrors...)...)
	return collectionlist.FilterMapList(endpoints, func(_ int, endpoint string) (string, bool) {
		trimmed := strings.TrimRight(strings.TrimSpace(endpoint), "/")
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	})
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
