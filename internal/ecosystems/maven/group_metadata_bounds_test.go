package maven_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
)

func TestMavenGroupMetadataBoundsCombinedInput(t *testing.T) {
	perMemberBytes := (16<<20)/2 + 1
	body := sizedCommentMetadata(perMemberBytes)
	first := metadataServer(t, body)
	second := metadataServer(t, body)
	t.Cleanup(first.Close)
	t.Cleanup(second.Close)

	service, _, _ := newTestServiceWithStores(
		context.Background(),
		t,
		map[string]config.DependencyUpstreamConfig{
			"first":  {Registry: first.URL},
			"second": {Registry: second.URL},
		},
		config.MavenGroupsConfig{"public": {Members: []string{"first", "second"}}},
	)
	_, err := service.GetGroup(context.Background(), maven.Request{
		Alias: "public",
		Tail:  "com/acme/demo/maven-metadata.xml",
	})
	if err == nil || !strings.Contains(err.Error(), "group metadata input exceeds") {
		t.Fatalf("combined metadata error = %v, want group input size limit", err)
	}
}

func TestMavenGroupMetadataBoundsMergedOutput(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	body := sizedPluginMetadata((16 << 20) - 1)
	upstream := metadataServer(t, body)
	t.Cleanup(upstream.Close)
	service, _, _ := newTestServiceWithStores(
		context.Background(),
		t,
		map[string]config.DependencyUpstreamConfig{"central": {Registry: upstream.URL}},
		config.MavenGroupsConfig{"public": {Members: []string{"central"}}},
	)
	_, err := service.GetGroup(context.Background(), maven.Request{
		Alias: "public",
		Tail:  "com/acme/demo/maven-metadata.xml",
	})
	if err == nil || !strings.Contains(err.Error(), "output exceeds") {
		t.Fatalf("merged metadata error = %v, want output size limit", err)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("oversized metadata retained temporary spool files: %v", entries)
	}
}

func TestMavenGroupMetadataAdmissionWaitsForCollectionButNotSlowClient(t *testing.T) {
	service, started, requests, release := newMavenGroupMetadataAdmissionService(t)
	defer release()
	firstResults := startMavenGroupMetadataRequests(t, service)
	waitForMavenGroupMetadataRequests(t, started)
	assertMavenGroupMetadataAdmissionWaits(t, service, requests)
	release()
	responses := collectMavenGroupMetadataResponses(t, firstResults)
	third := getMavenGroupMetadataResponse(t.Context(), service, "four")
	if third.err != nil {
		t.Fatalf("request after spooling blocked by unread responses: %v", third.err)
	}
	responses = append(responses, third.response)
	for _, response := range responses {
		closeMavenGroupResponse(t, response)
	}
}

type mavenGroupResult struct {
	response *maven.Response
	err      error
}

func newMavenGroupMetadataAdmissionService(
	t *testing.T,
) (*maven.Service, <-chan struct{}, *atomic.Int32, func()) {
	t.Helper()
	var requests atomic.Int32
	started := make(chan struct{}, 2)
	releaseChannel := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseChannel) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) <= 2 {
			started <- struct{}{}
			<-releaseChannel
		}
		w.Header().Set("Content-Type", "application/xml")
		if _, err := w.Write([]byte("<metadata><versioning><versions><version>1.0</version></versions></versioning></metadata>")); err != nil {
			return
		}
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(release)
	service, _, _ := newTestServiceWithStores(t.Context(), t,
		map[string]config.DependencyUpstreamConfig{"central": {Registry: upstream.URL}},
		config.MavenGroupsConfig{"public": {Members: []string{"central"}}},
	)
	return service, started, &requests, release
}

func startMavenGroupMetadataRequests(t *testing.T, service *maven.Service) <-chan mavenGroupResult {
	t.Helper()
	results := make(chan mavenGroupResult, 2)
	for _, name := range []string{"one", "two"} {
		go func(name string) {
			results <- getMavenGroupMetadataResponse(t.Context(), service, name)
		}(name)
	}
	return results
}

func waitForMavenGroupMetadataRequests(t *testing.T, started <-chan struct{}) {
	t.Helper()
	for range 2 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("two group metadata requests were not admitted")
		}
	}
}

func assertMavenGroupMetadataAdmissionWaits(t *testing.T, service *maven.Service, requests *atomic.Int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	queued := getMavenGroupMetadataResponse(ctx, service, "three")
	if queued.err == nil {
		closeMavenGroupResponse(t, queued.response)
		t.Fatal("third group metadata request passed the two-slot admission limit")
	}
	if !errors.Is(queued.err, context.DeadlineExceeded) {
		t.Fatalf("queued request error = %v, want deadline exceeded", queued.err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("upstream requests while two admitted fills were blocked = %d, want 2", got)
	}
}

func collectMavenGroupMetadataResponses(t *testing.T, results <-chan mavenGroupResult) []*maven.Response {
	t.Helper()
	responses := make([]*maven.Response, 0, 2)
	for range 2 {
		select {
		case item := <-results:
			if item.err != nil {
				t.Fatalf("admitted request failed: %v", item.err)
			}
			responses = append(responses, item.response)
		case <-time.After(5 * time.Second):
			t.Fatal("admitted metadata request did not finish")
		}
	}
	return responses
}

func getMavenGroupMetadataResponse(requestContext context.Context, service *maven.Service, name string) mavenGroupResult {
	response, err := service.GetGroup(requestContext, maven.Request{
		Alias: "public",
		Tail:  fmt.Sprintf("com/acme/%s/maven-metadata.xml", name),
	})
	return mavenGroupResult{response: response, err: err}
}

func TestMavenGroupMetadataResponseClosesSpools(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	upstream := metadataServer(t, "<metadata><versioning><versions><version>1.0</version></versions></versioning></metadata>")
	t.Cleanup(upstream.Close)
	service, _, _ := newTestServiceWithStores(
		context.Background(),
		t,
		map[string]config.DependencyUpstreamConfig{"central": {Registry: upstream.URL}},
		config.MavenGroupsConfig{"public": {Members: []string{"central"}}},
	)
	response, err := service.GetGroup(context.Background(), maven.Request{
		Alias: "public",
		Tail:  "com/acme/demo/maven-metadata.xml",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := responseBody(t, response)
	if !strings.Contains(body, "<version>1.0</version>") {
		t.Fatalf("merged metadata body = %q, missing expected version", body)
	}
	waitMavenGroupSpoolsClosed(t, tempDir)
}

func waitMavenGroupSpoolsClosed(t *testing.T, directory string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("temporary response spools remain after body close and cache completion: %v", filepath.Join(directory, entries[0].Name()))
		}
		time.Sleep(time.Millisecond)
	}
}

func closeMavenGroupResponse(t *testing.T, response *maven.Response) {
	t.Helper()
	if response == nil || response.Body == nil {
		return
	}
	if err := response.Body.Close(); err != nil {
		t.Errorf("close Maven group response body: %v", err)
	}
}
