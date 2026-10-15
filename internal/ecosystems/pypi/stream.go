package pypi

import (
	"io"
	"math"

	"github.com/arcgolabs/httpx"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
)

// Fiber's native body stream avoids buffering and preserves Content-Length.
// Context wrappers (including httpx catch-all routing) expose Unwrap.
func streamResponseBody(status int, size int64, body io.ReadCloser) httpx.ResponseStream {
	return func(ctx huma.Context) {
		ctx.SetStatus(status)
		write := func(writer io.Writer) {
			defer closeReadCloser(body, nil, "close pypi response body")
			flushResponse(writer)
			if _, err := io.Copy(flushingWriter{Writer: writer}, body); err != nil {
				return
			}
		}
		streamToAdapter(ctx, size, body, write)
	}
}

func streamToAdapter(ctx huma.Context, size int64, body io.ReadCloser, write func(io.Writer)) {
	for range 8 {
		if native, ok := ctx.(interface{ Unwrap() fiber.Ctx }); ok {
			response := native.Unwrap().Response()
			bodySize := -1
			if size >= 0 && size <= math.MaxInt32 {
				bodySize = int(size)
			}
			response.SetBodyStream(body, bodySize)
			response.ImmediateHeaderFlush = true
			return
		}
		unwrapper, ok := ctx.(interface{ Unwrap() huma.Context })
		if !ok {
			break
		}
		ctx = unwrapper.Unwrap()
	}
	write(ctx.BodyWriter())
}

type flushingWriter struct{ io.Writer }

func (w flushingWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err == nil {
		flushResponse(w.Writer)
	}
	return n, wrapError(err, "write pypi response")
}

func flushResponse(writer io.Writer) {
	if flusher, ok := writer.(interface{ Flush() }); ok {
		flusher.Flush()
	}
}
