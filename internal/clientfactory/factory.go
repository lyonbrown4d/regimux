// Package clientfactory centralizes HTTP client construction for ecosystem proxies.
package clientfactory

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arcgolabs/clientx"
	clienthttp "github.com/arcgolabs/clientx/http"
	appconfig "github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/oops"
)

const defaultUserAgent = "regimux/dev"

type Factory struct {
	logger *slog.Logger
}

type Config struct {
	BaseURL            string
	Timeout            time.Duration
	UserAgent          string
	HTTP2              bool
	TLSEnabled         bool
	InsecureSkipVerify bool
	ServerName         string
	Retry              clientx.RetryConfig
	Component          string
}

func New(logger *slog.Logger) *Factory {
	if logger == nil {
		logger = slog.Default()
	}
	return &Factory{logger: logger.With("component", "clientfactory")}
}

func (f *Factory) HTTP(cfg Config) (clienthttp.Client, error) {
	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	client, err := clienthttp.New(clienthttp.Config{
		BaseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		Timeout:   cfg.Timeout,
		UserAgent: userAgent,
		Retry:     cfg.Retry,
		TLS: clientx.TLSConfig{
			Enabled:            cfg.TLSEnabled,
			InsecureSkipVerify: cfg.InsecureSkipVerify,
			ServerName:         cfg.ServerName,
		},
	}, f.options(cfg.Component)...)
	if err != nil {
		return nil, wrapError(err, "create http client")
	}
	if cfg.Timeout == 0 {
		client.Raw().SetTimeout(0)
	}
	raw := client.Raw().Client()
	configureHTTP2(raw, cfg.HTTP2)
	raw.CheckRedirect = StripAuthOnCrossHostRedirect
	return client, nil
}

func (f *Factory) RawHTTP(cfg Config) (*http.Client, error) {
	client, err := f.HTTP(cfg)
	if err != nil {
		return nil, err
	}
	return client.Raw().Client(), nil
}

func (f *Factory) UpstreamHTTP(cfg appconfig.UpstreamConfig, baseURL, component string) (clienthttp.Client, error) {
	return f.HTTP(ForUpstream(cfg, baseURL, component))
}

func (f *Factory) RawUpstreamHTTP(cfg appconfig.UpstreamConfig, baseURL, component string) (*http.Client, error) {
	return f.RawHTTP(ForUpstream(cfg, baseURL, component))
}

func ForUpstream(cfg appconfig.UpstreamConfig, baseURL, component string) Config {
	return Config{
		BaseURL:    NormalizeBaseURL(baseURL),
		Timeout:    cfg.HTTP.Timeout,
		UserAgent:  defaultUserAgent,
		HTTP2:      cfg.HTTP.HTTP2.Enabled,
		TLSEnabled: cfg.HTTP.TLS.Enabled,
		Retry: clientx.RetryConfig{
			Enabled:    cfg.HTTP.Retry.Enabled,
			MaxRetries: cfg.HTTP.Retry.MaxRetries,
			WaitMin:    cfg.HTTP.Retry.WaitMin,
			WaitMax:    cfg.HTTP.Retry.WaitMax,
		},
		InsecureSkipVerify: cfg.HTTP.TLS.InsecureSkipVerify,
		ServerName:         cfg.HTTP.TLS.ServerName,
		Component:          component,
	}
}

func NormalizeBaseURL(rawURL string) string {
	rawURL = strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return rawURL
}

func (f *Factory) options(component string) []clienthttp.Option {
	if f == nil || f.logger == nil {
		return nil
	}
	logger := f.logger
	if component = strings.TrimSpace(component); component != "" {
		logger = logger.With("client_component", component)
	}
	return []clienthttp.Option{
		clienthttp.WithHooks(clientx.NewLoggingHook(
			logger,
			clientx.WithLoggingHookAddress(false),
		)),
	}
}

func configureHTTP2(client *http.Client, enabled bool) {
	if client == nil {
		return
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == nil {
		return
	}
	if enabled {
		transport.ForceAttemptHTTP2 = true
		transport.TLSNextProto = nil
		return
	}
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
}

func StripAuthOnCrossHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if req.URL.Host != via[0].URL.Host {
		req.Header.Del(distribution.HeaderAuthorization)
	}
	if len(via) >= 5 {
		return http.ErrUseLastResponse
	}
	return nil
}

func wrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return oops.In("clientfactory").Wrapf(err, "%s", message)
}
