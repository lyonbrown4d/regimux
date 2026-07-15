package maven_test

import (
	"context"
	"encoding/xml"
	"slices"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
)

func TestMavenGroupMergesPluginMetadata(t *testing.T) {
	ctx := context.Background()
	first := metadataServer(t, `<metadata>
  <groupId>com.acme</groupId>
  <plugins>
    <plugin>
      <name>Spring Plugin</name>
      <prefix>spring</prefix>
      <artifactId>spring-maven-plugin</artifactId>
    </plugin>
  </plugins>
</metadata>`)
	second := metadataServer(t, `<metadata>
  <groupId>com.acme</groupId>
  <plugins>
    <plugin>
      <name>Versions Plugin</name>
      <prefix>versions</prefix>
      <artifactId>versions-maven-plugin</artifactId>
    </plugin>
  </plugins>
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
		Tail:  "com/acme/maven-metadata.xml",
	})
	requireNoError(t, "plugin metadata get", err)

	var document struct {
		Plugins []struct {
			Prefix     string `xml:"prefix"`
			ArtifactID string `xml:"artifactId"`
		} `xml:"plugins>plugin"`
	}
	if err := xml.Unmarshal([]byte(responseBody(t, response)), &document); err != nil {
		t.Fatalf("decode plugin metadata: %v", err)
	}
	prefixes := make([]string, 0, len(document.Plugins))
	for _, plugin := range document.Plugins {
		prefixes = append(prefixes, plugin.Prefix)
	}
	if !slices.Equal(prefixes, []string{"spring", "versions"}) {
		t.Fatalf("plugin prefixes = %v, want [spring versions]", prefixes)
	}
}
