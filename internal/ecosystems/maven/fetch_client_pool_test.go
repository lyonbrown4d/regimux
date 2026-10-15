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
