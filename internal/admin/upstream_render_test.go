package admin_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/admin"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestServiceRendersScopedDependencyUpstreamStats(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultConfig()
	metadata := newMetadataStore(t)
	now := time.Now().UTC()
	key := meta.PullKey{
		Alias:      ecosystem.ScopedAlias(ecosystem.Go, "default"),
		Repository: "example.com/acme/lib",
		Reference:  "v1.0.0",
	}
	if _, err := metadata.RecordPull(ctx, key, now.Add(-time.Minute)); err != nil {
		t.Fatalf("record go pull: %v", err)
	}
	if _, err := metadata.RecordPull(ctx, key, now); err != nil {
		t.Fatalf("record second go pull: %v", err)
	}

	service := admin.NewService(admin.Dependencies{
		Config:   cfg,
		Metadata: metadata,
		Runtimes: newAdminTestRuntimes(cfg),
		Version:  build.Version("test-version"),
		Messages: newAdminMessages(t),
	})
	views, err := admin.NewTemplateEngine(newAdminMessages(t))
	if err != nil {
		t.Fatalf("new template engine: %v", err)
	}

	app := fiber.New(fiber.Config{Views: views})
	service.RegisterFiber(app)

	resp := adminRequest(t, app, "/admin/upstreams", "", "")
	defer closeResponseBody(t, resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read upstream response: %v", err)
	}
	row := upstreamHTMLRow(t, string(body), "go/default")
	if !strings.Contains(row, "https://proxy.golang.org") || !strings.Contains(row, ">2</td>") {
		t.Fatalf("go/default row did not include scoped metadata: %s", row)
	}
}

func upstreamHTMLRow(t *testing.T, body, displayAlias string) string {
	t.Helper()

	marker := ">" + displayAlias + "</td>"
	markerIndex := strings.Index(body, marker)
	if markerIndex < 0 {
		t.Fatalf("response did not contain upstream row %q: %s", displayAlias, body)
	}
	start := strings.LastIndex(body[:markerIndex], "<tr")
	if start < 0 {
		t.Fatalf("upstream row %q has no opening tr: %s", displayAlias, body)
	}
	endOffset := strings.Index(body[markerIndex:], "</tr>")
	if endOffset < 0 {
		t.Fatalf("upstream row %q has no closing tr: %s", displayAlias, body)
	}
	return body[start : markerIndex+endOffset+len("</tr>")]
}
