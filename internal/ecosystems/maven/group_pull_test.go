package maven_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestMavenGroupRecordsPullForPrefetchCandidates(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeResponse(t, w, "group jar")
	}))
	t.Cleanup(upstream.Close)

	service, metadata, _ := newTestServiceWithStores(
		ctx,
		t,
		map[string]config.DependencyUpstreamConfig{
			"central": {Registry: upstream.URL},
		},
		config.MavenGroupsConfig{
			"public": {Members: []string{"central"}},
		},
	)
	response, err := service.GetGroup(ctx, maven.Request{
		Alias: "public",
		Tail:  "com/acme/demo/1.0/demo-1.0.jar",
	})
	requireNoError(t, "group get", err)
	assertBody(t, response, "group jar")

	pull, ok, err := metadata.Pull(ctx, meta.PullKey{
		Alias:      "maven/public",
		Repository: "com/acme/demo/1.0",
		Reference:  "demo-1.0.jar",
	})
	requireNoError(t, "lookup group pull", err)
	if !ok {
		t.Fatal("group pull was not recorded")
	}
	if pull.Count != 1 || pull.LastPullAt.IsZero() {
		t.Fatalf("unexpected group pull record: %#v", pull)
	}
}
