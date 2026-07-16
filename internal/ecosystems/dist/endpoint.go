package dist

import (
	"context"
	"net/http"
	"strconv"

	"github.com/arcgolabs/httpx"
	"github.com/danielgtaylor/huma/v2"
	"github.com/lyonbrown4d/regimux/internal/artifactstream"
)

type Endpoint struct {
	service *Service
}

type input struct {
	Alias string         `path:"alias"`
	Tail  httpx.PathTail `path:"tail"`
	Range string         `header:"Range"`
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

func (e *Endpoint) EndpointSpec() httpx.EndpointSpec {
	return httpx.EndpointSpec{
		Tags:       httpx.Tags("dist"),
		Security:   httpx.SecurityRequirements(),
		Parameters: httpx.Parameters(),
		Extensions: httpx.Extensions(nil),
	}
}

func (e *Endpoint) Register(registrar httpx.Registrar) {
	group := registrar.Scope()
	httpx.MustGroupGet(group, "dist/{alias}/{tail...}", e.get)
	httpx.MustGroupRoute(group, http.MethodHead, "dist/{alias}/{tail...}", e.head)
}

func (e *Endpoint) get(ctx context.Context, input *input) (*output, error) {
	return e.dispatch(ctx, input, http.MethodGet)
}

func (e *Endpoint) head(ctx context.Context, input *input) (*output, error) {
	return e.dispatch(ctx, input, http.MethodHead)
}

func (e *Endpoint) dispatch(ctx context.Context, in *input, method string) (*output, error) {
	if in == nil {
		return plainError(http.StatusBadRequest, "dist proxy input is required"), nil
	}
	resp, err := e.service.Get(ctx, Request{
		Alias:  in.Alias,
		Tail:   in.Tail.String(),
		Method: method,
		Range:  in.Range,
	})
	if err != nil {
		return plainError(statusFromError(err), err.Error()), nil
	}
	return e.outputFromResponse(ctx, in, method, resp), nil
}

func (e *Endpoint) outputFromResponse(ctx context.Context, in *input, method string, resp *Response) *output {
	if resp == nil {
		return plainError(http.StatusBadGateway, "dist proxy response is empty")
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
	logger := e.service.logger
	out.Body = streamWithStatus(resp.Status, func(streamCtx huma.Context) {
		defer closeReadCloser(resp.Body, logger, "close dist response body")
		artifactstream.Copy(ctx, artifactstream.CopyRequest{
			Destination:   streamCtx.BodyWriter(),
			Source:        resp.Body,
			Logger:        logger,
			Ecosystem:     "dist",
			Alias:         in.Alias,
			Reference:     in.Tail.String(),
			Cache:         resp.Cache,
			ExpectedBytes: resp.Size,
		})
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
)
