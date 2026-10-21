package artifactstream_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/arcgolabs/httpx"
	fiberadapter "github.com/arcgolabs/httpx/adapter/fiber"
	stdadapter "github.com/arcgolabs/httpx/adapter/std"
	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/artifactstream"
)

func TestResponseStreamsHeadersBeforeBodyCompletes(t *testing.T) {
	for _, adapter := range []string{"std", "fiber"} {
		for _, size := range []int64{10, -1} {
			t.Run(adapter+"/size_"+strconv.FormatInt(size, 10), func(t *testing.T) {
				checkResponseStream(t, adapter, size)
			})
		}
	}
}

func checkResponseStream(t *testing.T, adapter string, size int64) {
	t.Helper()
	gate := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	defer release()
	reader := &gatedBody{prefix: []byte("first-"), tail: []byte("last"), gate: gate, closed: make(chan struct{})}
	client, url := startResponseServer(t, adapter, reader, size)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		release()
		t.Fatal(err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	assertResponseStreamHeaders(t, resp, size)
	release()
	finishResponseStream(ctx, t, resp, reader)
}

func assertResponseStreamHeaders(t *testing.T, resp *http.Response, size int64) {
	t.Helper()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusPartialContent)
	}
	if size >= 0 && resp.ContentLength != size {
		t.Fatalf("content length = %d, want %d", resp.ContentLength, size)
	}
	if size < 0 && resp.ContentLength != -1 {
		t.Fatalf("content length = %d, want unknown (-1)", resp.ContentLength)
	}
}

func finishResponseStream(ctx context.Context, t *testing.T, resp *http.Response, reader *gatedBody) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "first-last" {
		t.Fatalf("body = %q, want %q", body, "first-last")
	}
	select {
	case <-reader.closed:
	case <-ctx.Done():
		t.Fatal("response body was not closed")
	}
	if count := reader.closeCount(); count != 1 {
		t.Fatalf("body close count = %d, want 1", count)
	}
}

func startResponseServer(t *testing.T, adapter string, body io.ReadCloser, size int64) (*http.Client, string) {
	t.Helper()
	e := responseEndpoint{body: body, size: size}
	if adapter == "std" {
		a := stdadapter.New(nil)
		s := httpx.New(httpx.WithAdapter(a))
		s.RegisterOnly(e)
		server := httptest.NewServer(a.Router())
		t.Cleanup(server.Close)
		return server.Client(), server.URL + "/stream"
	}
	app := fiber.New()
	a := fiberadapter.New(app)
	s := httpx.New(httpx.WithAdapter(a))
	s.RegisterOnly(e)
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := app.ShutdownWithContext(shutdownCtx); err != nil {
			t.Errorf("shutdown Fiber: %v", err)
		}
		if err := <-stopped; err != nil {
			t.Errorf("Fiber listener: %v", err)
		}
	})
	return &http.Client{Timeout: 3 * time.Second}, "http://" + listener.Addr().String() + "/stream"
}

type responseEndpoint struct {
	body io.ReadCloser
	size int64
}

type responseInput struct{}

type responseOutput struct {
	Status        int
	ContentLength string `header:"Content-Length"`
	Body          httpx.ResponseStream
}

func (e responseEndpoint) EndpointSpec() httpx.EndpointSpec {
	return httpx.EndpointSpec{Tags: httpx.Tags("stream test"), Security: httpx.SecurityRequirements(), Parameters: httpx.Parameters(), Extensions: httpx.Extensions(nil)}
}

func (e responseEndpoint) Register(registrar httpx.Registrar) {
	httpx.MustGroupGet(registrar.Scope(), "stream", e.get)
}

func (e responseEndpoint) get(ctx context.Context, _ *responseInput) (*responseOutput, error) {
	length := ""
	if e.size >= 0 {
		length = strconv.FormatInt(e.size, 10)
	}
	return &responseOutput{Status: http.StatusPartialContent, ContentLength: length, Body: artifactstream.Response(ctx, artifactstream.ResponseRequest{Status: http.StatusPartialContent, Size: e.size, Body: e.body})}, nil
}

type gatedBody struct {
	prefix []byte
	tail   []byte
	gate   <-chan struct{}
	closed chan struct{}
	once   sync.Once
	mu     sync.Mutex
	count  int
	phase  int
}

func (r *gatedBody) Read(p []byte) (int, error) {
	if r.phase == 0 {
		r.phase = 1
		return copy(p, r.prefix), nil
	}
	if r.phase == 1 {
		<-r.gate
		r.phase = 2
		return copy(p, r.tail), nil
	}
	return 0, io.EOF
}

func (r *gatedBody) Close() error {
	r.mu.Lock()
	r.count++
	r.mu.Unlock()
	r.once.Do(func() { close(r.closed) })
	return nil
}

func (r *gatedBody) closeCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}
