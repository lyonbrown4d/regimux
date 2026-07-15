package maven

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/arcgolabs/httpx"
	"github.com/danielgtaylor/huma/v2"
)

type Endpoint struct {
	service *Service
}

type input struct {
	Alias string         `path:"alias"`
	Tail  httpx.PathTail `path:"tail"`
}

type output struct {
	Status           int
	ContentType      string `header:"Content-Type"`
	ContentLength    string `header:"Content-Length"`
	ETag             string `header:"ETag"`
	LastModified     string `header:"Last-Modified"`
	XMirrorCache     string `header:"X-Mirror-Cache"`
	XRegimuxUpstream string `header:"X-Regimux-Upstream"`
	Body             httpx.ResponseStream
}

type serviceGet func(context.Context, Request) (*Response, error)

func NewEndpoint(service *Service) *Endpoint {
	return &Endpoint{service: service}
}

func (e *Endpoint) EndpointSpec() httpx.EndpointSpec {
	return httpx.EndpointSpec{
		Tags:       httpx.Tags("maven"),
		Security:   httpx.SecurityRequirements(),
		Parameters: httpx.Parameters(),
		Extensions: httpx.Extensions(nil),
	}
}

func (e *Endpoint) Register(registrar httpx.Registrar) {
	group := registrar.Scope()
	httpx.MustGroupGet(group, "maven/{alias}/{tail...}", e.get)
	httpx.MustGroupRoute(group, http.MethodHead, "maven/{alias}/{tail...}", e.head)
	httpx.MustGroupGet(group, "maven-group/{alias}/{tail...}", e.getGroup)
	httpx.MustGroupRoute(group, http.MethodHead, "maven-group/{alias}/{tail...}", e.headGroup)
}

func (e *Endpoint) get(ctx context.Context, input *input) (*output, error) {
	return e.dispatch(ctx, input, http.MethodGet, e.service.Get)
}

func (e *Endpoint) head(ctx context.Context, input *input) (*output, error) {
	return e.dispatch(ctx, input, http.MethodHead, e.service.Get)
}

func (e *Endpoint) getGroup(ctx context.Context, input *input) (*output, error) {
	return e.dispatch(ctx, input, http.MethodGet, e.service.GetGroup)
}

func (e *Endpoint) headGroup(ctx context.Context, input *input) (*output, error) {
	return e.dispatch(ctx, input, http.MethodHead, e.service.GetGroup)
}

func (e *Endpoint) dispatch(
	ctx context.Context,
	in *input,
	method string,
	get serviceGet,
) (*output, error) {
	if in == nil {
		return plainError(http.StatusBadRequest, "maven proxy input is required"), nil
	}
	resp, err := get(ctx, Request{
		Alias:  in.Alias,
		Tail:   in.Tail.String(),
		Method: method,
	})
	if err != nil {
		return plainError(statusFromError(err), err.Error()), nil
	}
	return outputFromResponse(method, resp), nil
}

func outputFromResponse(method string, resp *Response) *output {
	if resp == nil {
		return plainError(http.StatusBadGateway, "maven proxy response is empty")
	}
	out := &output{
		Status:           resp.Status,
		ContentType:      resp.Headers.Get("Content-Type"),
		ContentLength:    resp.Headers.Get("Content-Length"),
		ETag:             resp.Headers.Get("ETag"),
		LastModified:     resp.Headers.Get("Last-Modified"),
		XMirrorCache:     resp.Headers.Get(headerMirrorCache),
		XRegimuxUpstream: resp.Headers.Get(resolvedUpstreamHeader),
		Body:             streamWithStatus(resp.Status, nil),
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
	out.Body = streamWithStatus(resp.Status, httpx.StreamWriter(func(writer io.Writer) {
		defer closeReadCloser(resp.Body, nil, "close maven response body")
		if _, err := io.Copy(writer, resp.Body); err != nil {
			return
		}
	}))
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
)
