package ecosystem_test

import (
	"testing"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func TestConfiguredUpstreamsNormalizesRuntimeEcosystem(t *testing.T) {
	upstreams := collectionmapping.NewOrderedMap[string, config.UpstreamConfig]()
	upstreams.Set("default", config.UpstreamConfig{Registry: "https://proxy.golang.org"})
	runtimes := collectionlist.NewList[ecosystem.Runtime](
		ecosystem.NewConfigRuntime(ecosystem.Go, upstreams),
		nilUpstreamProviderRuntime{},
	)

	got := ecosystem.ConfiguredUpstreams(runtimes)
	if got.Len() != 1 {
		t.Fatalf("upstream count = %d, want 1", got.Len())
	}
	upstream := got.Values()[0]
	if upstream.Ecosystem != ecosystem.Go || upstream.Alias != "default" {
		t.Fatalf("upstream = %+v, want go/default", upstream)
	}
}

type nilUpstreamProviderRuntime struct{}

func (nilUpstreamProviderRuntime) Name() string {
	return "nil"
}

func (nilUpstreamProviderRuntime) Upstreams() *collectionlist.List[ecosystem.Upstream] {
	return nil
}
