package maven_test

import (
	"context"
	"encoding/xml"
	"slices"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
)

func TestMavenGroupMergesReleaseMetadata(t *testing.T) {
	ctx := context.Background()
	first := metadataServer(t, `<?xml version="1.0"?>
<metadata>
  <groupId>com.acme</groupId>
  <artifactId>demo</artifactId>
  <versioning>
    <latest>1.2-RC1</latest>
    <release>1.0</release>
    <versions><version>1.0</version><version>1.2-RC1</version></versions>
    <lastUpdated>20260701000000</lastUpdated>
  </versioning>
</metadata>`)
	second := metadataServer(t, `<?xml version="1.0"?>
<metadata>
  <groupId>com.acme</groupId>
  <artifactId>demo</artifactId>
  <versioning>
    <latest>1.2</latest>
    <release>1.2</release>
    <versions><version>1.1</version><version>1.2</version></versions>
    <lastUpdated>20260702000000</lastUpdated>
  </versioning>
</metadata>`)
	t.Cleanup(first.Close)
	t.Cleanup(second.Close)

	service, _, _ := newTestServiceWithStores(
		ctx,
		t,
		map[string]config.DependencyUpstreamConfig{
			"internal": {Registry: first.URL},
			"central":  {Registry: second.URL},
		},
		config.MavenGroupsConfig{
			"public": {Members: []string{"internal", "central"}},
		},
	)
	response, err := service.GetGroup(ctx, maven.Request{
		Alias: "public",
		Tail:  "com/acme/demo/maven-metadata.xml",
	})
	requireNoError(t, "merged metadata get", err)
	if response.Headers.Get(resolvedUpstreamHeaderName) != "internal,central" {
		t.Fatalf(
			"resolved upstreams = %q, want internal,central",
			response.Headers.Get(resolvedUpstreamHeaderName),
		)
	}
	assertMergedReleaseMetadata(t, response)
}

func assertMergedReleaseMetadata(t *testing.T, response *maven.Response) {
	t.Helper()
	var document struct {
		Versioning struct {
			Latest      string   `xml:"latest"`
			Release     string   `xml:"release"`
			Versions    []string `xml:"versions>version"`
			LastUpdated string   `xml:"lastUpdated"`
		} `xml:"versioning"`
	}
	if err := xml.Unmarshal([]byte(responseBody(t, response)), &document); err != nil {
		t.Fatalf("decode merged metadata: %v", err)
	}
	if document.Versioning.Latest != "1.2" || document.Versioning.Release != "1.2" {
		t.Fatalf(
			"latest/release = %q/%q, want 1.2/1.2",
			document.Versioning.Latest,
			document.Versioning.Release,
		)
	}
	wantVersions := []string{"1.0", "1.1", "1.2-RC1", "1.2"}
	if !slices.Equal(document.Versioning.Versions, wantVersions) {
		t.Fatalf("versions = %v, want %v", document.Versioning.Versions, wantVersions)
	}
	if document.Versioning.LastUpdated != "20260702000000" {
		t.Fatalf(
			"lastUpdated = %q, want 20260702000000",
			document.Versioning.LastUpdated,
		)
	}
}
