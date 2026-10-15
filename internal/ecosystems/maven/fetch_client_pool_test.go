//nolint:testpackage // Client pool tests inspect private lifecycle state.
package maven

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestClientForReusesClientPerEndpoint(t *testing.T) {
	service := &Service{factory: clientfactory.New(nil)}
	cfg := config.UpstreamConfig{}

	first, err := service.clientFor(cfg, "https://repo.example.com")
	if err != nil {
		t.Fatalf("create first client: %v", err)
	}
	second, err := service.clientFor(cfg, "https://repo.example.com")
	if err != nil {
		t.Fatalf("create second client: %v", err)
	}
	if first.Raw() != second.Raw() {
		t.Fatal("clientFor created a different raw client for the same endpoint")
	}
}

func TestClientForDoesNotSharePoolAcrossServices(t *testing.T) {
	factory := clientfactory.New(nil)
	firstService := &Service{factory: factory}
	secondService := &Service{factory: factory}
	first, err := firstService.clientFor(config.UpstreamConfig{}, "https://repo.example.com")
	if err != nil {
		t.Fatalf("create first service client: %v", err)
	}
	second, err := secondService.clientFor(config.UpstreamConfig{}, "https://repo.example.com")
	if err != nil {
		t.Fatalf("create second service client: %v", err)
	}
	if first.Raw() == second.Raw() {
		t.Fatal("separate Maven services shared an upstream client")
	}
}

func TestServiceCloseReleasesPoolAndRejectsNewClients(t *testing.T) {
	service := &Service{factory: clientfactory.New(nil)}
	if _, err := service.clientFor(config.UpstreamConfig{}, "https://repo.example.com"); err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("close service: %v", err)
	}
	if len(service.clients) != 0 {
		t.Fatalf("clients after close = %d, want 0", len(service.clients))
	}
	if _, err := service.clientFor(config.UpstreamConfig{}, "https://repo.example.com"); err == nil {
		t.Fatal("clientFor succeeded after service close")
	}
}
