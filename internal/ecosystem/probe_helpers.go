package ecosystem

import (
	"net/http"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func probeEndpoints(cfg config.UpstreamConfig) *collectionlist.List[string] {
	endpoints := make([]string, 0, 1+len(cfg.Mirrors))
	if strings.TrimSpace(cfg.Registry) != "" {
		endpoints = append(endpoints, strings.TrimRight(strings.TrimSpace(cfg.Registry), "/"))
	}
	for _, mirror := range cfg.Mirrors {
		if strings.TrimSpace(mirror) != "" {
			endpoints = append(endpoints, strings.TrimRight(strings.TrimSpace(mirror), "/"))
		}
	}
	return collectionlist.NewList(endpoints...)
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
