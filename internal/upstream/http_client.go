package upstream

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/arcgolabs/clientx"
	clienthttp "github.com/arcgolabs/clientx/http"
	"resty.dev/v3"
)

const defaultUserAgent = "regimux/dev"

func newHTTPClient(cfg Config) (clienthttp.Client, error) {
	httpClient, err := clienthttp.New(clienthttp.Config{
		BaseURL:   strings.TrimRight(cfg.Registry, "/"),
		Timeout:   cfg.HTTP.Timeout,
		UserAgent: defaultUserAgent,
		Retry: clientx.RetryConfig{
			Enabled:    cfg.HTTP.Retry.Enabled,
			MaxRetries: cfg.HTTP.Retry.MaxRetries,
			WaitMin:    cfg.HTTP.Retry.WaitMin,
			WaitMax:    cfg.HTTP.Retry.WaitMax,
		},
		TLS: clientx.TLSConfig{
			Enabled:            cfg.HTTP.TLS.Enabled,
			InsecureSkipVerify: cfg.HTTP.TLS.InsecureSkipVerify,
			ServerName:         cfg.HTTP.TLS.ServerName,
		},
	})
	if err != nil {
		return nil, wrapError(err, "create upstream http client")
	}
	if cfg.HTTP.Timeout == 0 {
		// Preserve RegiMux's previous no-client-timeout behavior. Per-request
		// cancellation still flows through context.
		httpClient.Raw().SetTimeout(0)
	}
	httpClient.Raw().Client().CheckRedirect = stripAuthOnCrossHostRedirect
	return httpClient, nil
}

func stripAuthOnCrossHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if req.URL.Host != via[0].URL.Host {
		req.Header.Del("Authorization")
	}
	if len(via) >= 5 {
		return http.ErrUseLastResponse
	}
	return nil
}

func prepareRequest(req *resty.Request, cfg Config) {
	req.SetHeader("User-Agent", defaultUserAgent)
	switch strings.ToLower(cfg.Auth.Type) {
	case "bearer":
		if cfg.Auth.Token != "" {
			req.SetHeader("Authorization", "Bearer "+cfg.Auth.Token)
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
		return base + "/v2/" + strings.Trim(repo, "/") + "/" + operation
	}
	return base + "/v2/" + strings.Trim(repo, "/") + "/" + operation + "/" + value
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
	requestURL := registryURL(registry, req.Repo, "tags/list", "")
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
