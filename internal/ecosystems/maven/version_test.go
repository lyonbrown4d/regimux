package maven_test

import (
	"context"
	"encoding/xml"
	"slices"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
)

func TestMavenGroupUsesMavenVersionOrdering(t *testing.T) {
	ctx := context.Background()
	first := metadataServer(
		t,
		"<metadata><versioning><versions><version>1.9</version><version>1.0-rc1</version></versions></versioning></metadata>",
	)
	second := metadataServer(
		t,
		"<metadata><versioning><versions><version>1.10</version><version>1.0</version></versions></versioning></metadata>",
	)
	t.Cleanup(first.Close)
	t.Cleanup(second.Close)

	service, _, _ := newTestServiceWithStores(
		ctx,
		t,
		map[string]config.DependencyUpstreamConfig{
			"first":  {Registry: first.URL},
			"second": {Registry: second.URL},
		},
		config.MavenGroupsConfig{
			"public": {Members: []string{"first", "second"}},
		},
	)
	response, err := service.GetGroup(ctx, maven.Request{
		Alias: "public",
		Tail:  "com/acme/demo/maven-metadata.xml",
	})
	requireNoError(t, "ordered metadata get", err)

	var document struct {
		Versions []string `xml:"versioning>versions>version"`
	}
	if err := xml.Unmarshal([]byte(responseBody(t, response)), &document); err != nil {
		t.Fatalf("decode ordered metadata: %v", err)
	}
	want := []string{"1.0-rc1", "1.0", "1.9", "1.10"}
	if !slices.Equal(document.Versions, want) {
		t.Fatalf("versions = %v, want %v", document.Versions, want)
	}
}
