package upstream

import (
	"log/slog"
	"net/url"
	"strings"

	"github.com/arcgolabs/clientx"
	clienthttp "github.com/arcgolabs/clientx/http"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"resty.dev/v3"
)

const (
	defaultUserAgent       = "regimux/dev"
	registryAPIVersionPath = "/v2/"
	tagsPath               = "tags/list"
)

func newHTTPClient(cfg Config, logger *slog.Logger, factories ...*clientfactory.Factory) (clienthttp.Client, error) {
	factory := clientfactory.New(logger)
	if len(factories) > 0 && factories[0] != nil {
		factory = factories[0]
	}
	httpClient, err := factory.HTTP(clientfactory.Config{
		BaseURL:   cfg.Registry,
		Timeout:   cfg.HTTP.Timeout,
		UserAgent: defaultUserAgent,
		// RegiMux handles upstream retries in request.go so status-based retry,
		// body draining, metrics, and failover all share one attempt model.
		Retry:              clientx.RetryConfig{Enabled: false},
		HTTP2:              cfg.HTTP.HTTP2.Enabled,
		TLSEnabled:         cfg.HTTP.TLS.Enabled,
		InsecureSkipVerify: cfg.HTTP.TLS.InsecureSkipVerify,
		ServerName:         cfg.HTTP.TLS.ServerName,
		Component:          "upstream.clientx",
	})
	if err != nil {
		return nil, wrapError(err, "create upstream http client")
	}
	return httpClient, nil
}

func prepareRequest(req *resty.Request, cfg Config) {
	req.SetHeader(distribution.HeaderUserAgent, defaultUserAgent)
	switch strings.ToLower(cfg.Auth.Type) {
	case strings.ToLower(distribution.AuthSchemeBearer):
		if cfg.Auth.Token != "" {
			req.SetHeader(distribution.HeaderAuthorization, distribution.AuthSchemeBearer+" "+cfg.Auth.Token)
		}
	case "basic", "dockerhub":
		if cfg.Auth.Username != "" || cfg.Auth.Password != "" {
			req.SetBasicAuth(cfg.Auth.Username, cfg.Auth.Password)
		}
	}
}

func withHeader(key, value string) requestOption {
	return func(req *resty.Request) {
		req.SetHeader(key, value)
	}
}

func registryURL(registry, repo, operation, value string) string {
	base := strings.TrimRight(registry, "/")
	if value == "" {
		return base + registryAPIVersionPath + strings.Trim(repo, "/") + "/" + operation
	}
	return base + registryAPIVersionPath + strings.Trim(repo, "/") + "/" + operation + "/" + value
}

func pullRepositoryScope(repo string) string {
	return "repository:" + repo + ":pull"
}

func methodOr(method, fallback string) string {
	if method == "" {
		return fallback
	}
	return method
}

func tagsURL(registry string, req ListTagsRequest) (string, error) {
	requestURL := registryURL(registry, req.Repo, tagsPath, "")
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return "", wrapError(err, "parse upstream tags URL")
	}
	query := parsed.Query()
	if req.N != "" {
		query.Set("n", req.N)
	}
	if req.Last != "" {
		query.Set("last", req.Last)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
