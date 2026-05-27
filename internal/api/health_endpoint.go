package api

import (
	"context"

	"github.com/arcgolabs/httpx"
)

type HealthEndpoint struct{}

func NewHealthEndpoint() *HealthEndpoint {
	return &HealthEndpoint{}
}

func (e *HealthEndpoint) EndpointSpec() httpx.EndpointSpec {
	return endpointSpec("health")
}

func (e *HealthEndpoint) Register(registrar httpx.Registrar) {
	group := registrar.Scope()
	httpx.MustGroupGet(group, "healthz", e.health)
}

func (e *HealthEndpoint) health(context.Context, *struct{}) (*healthOutput, error) {
	out := &healthOutput{}
	out.Body.Status = "ok"
	return out, nil
}

type healthOutput struct {
	Body struct {
		Status string `json:"status"`
	} `json:"body"`
}
