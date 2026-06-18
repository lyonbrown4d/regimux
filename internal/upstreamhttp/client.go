// Package upstreamhttp adapts ecosystem upstream fetches to the shared clientx HTTP client.
package upstreamhttp

import (
	"context"
	"io"
	"net/http"
	"strings"

	clienthttp "github.com/arcgolabs/clientx/http"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/samber/oops"
	"resty.dev/v3"
)

type Request struct {
	Method  string
	URL     string
	Headers http.Header
	Auth    config.AuthConfig
}

type Response struct {
	Status  int
	Headers http.Header
	Body    io.ReadCloser
}

func NewClient(factory *clientfactory.Factory, cfg config.UpstreamConfig, baseURL, component string) (clienthttp.Client, error) {
	if factory == nil {
		factory = clientfactory.New(nil)
	}
	client, err := factory.UpstreamHTTP(cfg, baseURL, component)
	if err != nil {
		return nil, oops.Wrapf(err, "create upstream http client")
	}
	return client, nil
}

func Do(ctx context.Context, client clienthttp.Client, req Request) (*Response, error) {
	if client == nil {
		return nil, oops.In("upstreamhttp").Errorf("client is nil")
	}
	method := requestMethod(req.Method)
	restyReq := client.R().SetResponseDoNotParse(true)
	applyHeaders(restyReq, req.Headers)
	applyAuth(restyReq, req.Auth)

	resp, err := client.Execute(ctx, restyReq, method, req.URL)
	if err != nil {
		return nil, oops.Wrapf(err, "send upstream http request")
	}
	body := resp.Body
	if body == nil {
		body = http.NoBody
	}
	return &Response{
		Status:  resp.StatusCode(),
		Headers: resp.Header().Clone(),
		Body:    body,
	}, nil
}

func RawDo(ctx context.Context, client *http.Client, req Request) (*Response, error) {
	if client == nil {
		return nil, oops.In("upstreamhttp").Errorf("raw client is nil")
	}
	httpReq, err := http.NewRequestWithContext(ctx, requestMethod(req.Method), req.URL, http.NoBody)
	if err != nil {
		return nil, oops.Wrapf(err, "create upstream http request")
	}
	if req.Headers != nil {
		httpReq.Header = req.Headers.Clone()
	}
	applyRawAuth(httpReq, req.Auth)

	resp, err := client.Do(httpReq) //nolint:bodyclose // The response body is returned to the caller for streaming/materialization.
	if err != nil {
		return nil, oops.Wrapf(err, "send upstream http request")
	}
	body := resp.Body
	if body == nil {
		body = http.NoBody
	}
	return &Response{
		Status:  resp.StatusCode,
		Headers: resp.Header.Clone(),
		Body:    body,
	}, nil
}

func applyHeaders(req *resty.Request, headers http.Header) {
	if req == nil || len(headers) == 0 {
		return
	}
	req.SetHeaderMultiValues(headers)
}

func applyAuth(req *resty.Request, cfg config.AuthConfig) {
	if req == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "basic":
		req.SetBasicAuth(cfg.Username, cfg.Password)
	case "bearer":
		if token := strings.TrimSpace(cfg.Token); token != "" {
			req.SetAuthToken(token)
		}
	}
}

func applyRawAuth(req *http.Request, cfg config.AuthConfig) {
	if req == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "basic":
		req.SetBasicAuth(cfg.Username, cfg.Password)
	case "bearer":
		if token := strings.TrimSpace(cfg.Token); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
}

func requestMethod(method string) string {
	method = strings.TrimSpace(method)
	if method == "" {
		return http.MethodGet
	}
	return method
}
