package golang

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/arcgolabs/httpx"
	"github.com/danielgtaylor/huma/v2"
	"github.com/lyonbrown4d/regimux/internal/artifactstream"
	"github.com/lyonbrown4d/regimux/internal/transfer"
)

type Endpoint struct {
	service *Service
}

type RootEndpoint struct {
	endpoint *Endpoint
}

type input struct {
	Alias   string         `path:"alias"`
	Tail    httpx.PathTail `path:"tail"`
	Range   string         `header:"Range"`
	IfRange string         `header:"If-Range"`
}

type rootInput struct {
	Tail    httpx.PathTail `path:"tail"`
	Range   string         `header:"Range"`
	IfRange string         `header:"If-Range"`
}

type output struct {
	Status        int
	ContentType   string `header:"Content-Type"`
	ContentLength string `header:"Content-Length"`
	ContentRange  string `header:"Content-Range"`
	AcceptRanges  string `header:"Accept-Ranges"`
	ETag          string `header:"ETag"`
	LastModified  string `header:"Last-Modified"`
	XMirrorCache  string `header:"X-Mirror-Cache"`
	Body          httpx.ResponseStream
}

func NewEndpoint(service *Service) *Endpoint {
	return &Endpoint{service: service}
}

func NewRootEndpoint(service *Service) *RootEndpoint {
	return &RootEndpoint{endpoint: NewEndpoint(service)}
}

func (e *Endpoint) EndpointSpec() httpx.EndpointSpec {
	return httpx.EndpointSpec{
		Tags:       httpx.Tags("go"),
		Security:   httpx.SecurityRequirements(),
		Parameters: httpx.Parameters(),
		Extensions: httpx.Extensions(nil),
	}
}

func (e *Endpoint) Register(registrar httpx.Registrar) {
	group := registrar.Scope()
	httpx.MustGroupGet(group, "go/{alias}/{tail...}", e.get)
	httpx.MustGroupRoute(group, http.MethodHead, "go/{alias}/{tail...}", e.head)
}

func (e *RootEndpoint) EndpointSpec() httpx.EndpointSpec {
	if e == nil || e.endpoint == nil {
		return NewEndpoint(nil).EndpointSpec()
	}
	return e.endpoint.EndpointSpec()
}

func (e *RootEndpoint) Register(registrar httpx.Registrar) {
	endpoint := e.endpoint
	if endpoint == nil {
		endpoint = NewEndpoint(nil)
	}
	group := registrar.Scope()
	httpx.MustGroupGet(group, "{tail...}", endpoint.getRoot)
	httpx.MustGroupRoute(group, http.MethodHead, "{tail...}", endpoint.headRoot)
}

func (e *Endpoint) get(ctx context.Context, input *input) (*output, error) {
	return e.dispatch(ctx, input, http.MethodGet)
}

func (e *Endpoint) head(ctx context.Context, input *input) (*output, error) {
	return e.dispatch(ctx, input, http.MethodHead)
}

func (e *Endpoint) getRoot(ctx context.Context, input *rootInput) (*output, error) {
	return e.dispatchRoot(ctx, input, http.MethodGet)
}

func (e *Endpoint) headRoot(ctx context.Context, input *rootInput) (*output, error) {
	return e.dispatchRoot(ctx, input, http.MethodHead)
}

func (e *Endpoint) dispatch(ctx context.Context, in *input, method string) (*output, error) {
	if in == nil {
		return plainError(http.StatusBadRequest, "go proxy input is required"), nil
	}
	resp, err := e.service.Get(ctx, Request{
		Alias:  in.Alias,
		Tail:   in.Tail.String(),
		Method: method,
	})
	if err != nil {
		return plainError(statusFromError(err), err.Error()), nil
	}
	resp, err = applyClientRange(method, in.Range, in.IfRange, resp)
	if err != nil {
		return plainError(http.StatusBadGateway, err.Error()), nil
	}
	return outputFromResponse(ctx, in.Alias, in.Tail.String(), method, resp, e.service.logger), nil
}

func (e *Endpoint) dispatchRoot(ctx context.Context, in *rootInput, method string) (*output, error) {
	if in == nil {
		return plainError(http.StatusBadRequest, "go proxy input is required"), nil
	}
	if !isGoProxyTail(in.Tail.String()) {
		return plainError(http.StatusNotFound, "go proxy path not found"), nil
	}
	resp, err := e.service.Get(ctx, Request{
		Tail:   in.Tail.String(),
		Method: method,
	})
	if err != nil {
		return plainError(statusFromError(err), err.Error()), nil
	}
	resp, err = applyClientRange(method, in.Range, in.IfRange, resp)
	if err != nil {
		return plainError(http.StatusBadGateway, err.Error()), nil
	}
	return outputFromResponse(ctx, "", in.Tail.String(), method, resp, e.service.logger), nil
}

func applyClientRange(method, rangeHeader, ifRange string, response *Response) (*Response, error) {
	if response == nil {
		return nil, errors.New("go proxy response is empty")
	}
	selected, err := transfer.ApplyRange(method, rangeHeader, ifRange, transfer.Response{
		Status: response.Status, Headers: response.Headers, Body: response.Body, Size: response.Size,
	})
	if err != nil {
		return nil, wrapError(err, "select go proxy byte range")
	}
	response.Status, response.Headers, response.Body, response.Size = selected.Status, selected.Headers, selected.Body, selected.Size
	return response, nil
}

func outputFromResponse(ctx context.Context, alias, reference, method string, resp *Response, logger *slog.Logger) *output {
	if resp == nil {
		return plainError(http.StatusBadGateway, "go proxy response is empty")
	}
	out := &output{
		Status:        resp.Status,
		ContentType:   resp.Headers.Get("Content-Type"),
		ContentLength: resp.Headers.Get("Content-Length"),
		ContentRange:  resp.Headers.Get("Content-Range"),
		AcceptRanges:  resp.Headers.Get("Accept-Ranges"),
		ETag:          resp.Headers.Get("ETag"),
		LastModified:  resp.Headers.Get("Last-Modified"),
		XMirrorCache:  resp.Headers.Get(headerMirrorCache),
		Body:          streamWithStatus(resp.Status, nil),
	}
	if out.ContentType == "" {
		out.ContentType = resp.ContentType
	}
	if out.ContentLength == "" && resp.Size >= 0 {
		out.ContentLength = strconv.FormatInt(resp.Size, 10)
	}
	if method == http.MethodHead || resp.Body == nil || resp.Body == http.NoBody {
		return out
	}
	out.Body = artifactstream.Response(ctx, artifactstream.ResponseRequest{
		Status: resp.Status, Size: resp.Size, Body: resp.Body, Logger: logger,
		Ecosystem: "golang", Alias: alias, Reference: reference, Cache: resp.Cache,
	})
	return out
}

func plainError(status int, message string) *output {
	body := []byte(message + "\n")
	return &output{
		Status:        status,
		ContentType:   "text/plain; charset=utf-8",
		ContentLength: strconv.Itoa(len(body)),
		Body:          streamWithStatus(status, httpx.StreamBytes(body)),
	}
}

func streamWithStatus(status int, stream httpx.ResponseStream) httpx.ResponseStream {
	return func(ctx huma.Context) {
		if ctx == nil {
			return
		}
		ctx.SetStatus(status)
		if stream != nil {
			stream(ctx)
		}
	}
}

var (
	_ httpx.Endpoint             = (*Endpoint)(nil)
	_ httpx.EndpointSpecProvider = (*Endpoint)(nil)
	_ httpx.Endpoint             = (*RootEndpoint)(nil)
	_ httpx.EndpointSpecProvider = (*RootEndpoint)(nil)
)
