package artifactstream

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"

	"github.com/arcgolabs/httpx"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
)

// ResponseRequest transfers ownership of Body to the response transport.
type ResponseRequest struct {
	Status    int
	Size      int64
	Body      io.ReadCloser
	Logger    *slog.Logger
	Ecosystem string
	Alias     string
	Reference string
	Cache     string
}

// Response uses the adapter's native stream instead of Fiber's buffered writer.
// The transport closes Body on completion or a disconnected client.
func Response(ctx context.Context, req ResponseRequest) httpx.ResponseStream {
	if req.Body == nil {
		req.Body = http.NoBody
	}
	body := &responseBody{ctx: ctx, req: req}
	return func(streamCtx huma.Context) {
		if streamCtx == nil {
			body.close()
			return
		}
		streamCtx.SetStatus(req.Status)
		if setNativeResponse(streamCtx, req.Size, body) {
			return
		}
		defer body.close()
		writer := streamCtx.BodyWriter()
		flushResponse(writer)
		Copy(ctx, req.copyRequest(flushingWriter{Writer: writer}))
	}
}

func setNativeResponse(ctx huma.Context, size int64, body io.ReadCloser) bool {
	for range 8 {
		if native, ok := ctx.(interface{ Unwrap() fiber.Ctx }); ok {
			response := native.Unwrap().Response()
			bodySize := -1
			if size >= 0 && size <= math.MaxInt {
				bodySize = int(size)
			}
			response.SetBodyStream(body, bodySize)
			response.ImmediateHeaderFlush = true
			return true
		}
		unwrapper, ok := ctx.(interface{ Unwrap() huma.Context })
		if !ok {
			return false
		}
		ctx = unwrapper.Unwrap()
	}
	return false
}

func (req ResponseRequest) copyRequest(destination io.Writer) CopyRequest {
	return CopyRequest{
		Destination: destination, Source: req.Body, Logger: req.Logger,
		Ecosystem: req.Ecosystem, Alias: req.Alias, Reference: req.Reference,
		Cache: req.Cache, ExpectedBytes: req.Size,
	}
}

type flushingWriter struct{ io.Writer }

func (w flushingWriter) Write(payload []byte) (int, error) {
	n, err := w.Writer.Write(payload)
	if err != nil {
		return n, fmt.Errorf("write artifact response: %w", err)
	}
	flushResponse(w.Writer)
	return n, nil
}

func flushResponse(writer io.Writer) {
	if flusher, ok := writer.(interface{ Flush() }); ok {
		flusher.Flush()
	}
}
