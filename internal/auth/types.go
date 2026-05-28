// Package auth wires RegiMux registry access authentication and authorization.
package auth

import (
	"context"
	"time"

	"github.com/arcgolabs/authx"
)

const (
	ActionPull         = "pull"
	ActionRegistryPing = "registry:ping"

	ScopeTypeRepository = "repository"
)

type BasicCredential struct {
	Username string
	Password string
}

type TokenRequest struct {
	Username string
	Password string
	Service  string
	Scopes   []string
}

type TokenResponse struct {
	Token       string    `json:"token"`
	AccessToken string    `json:"access_token"`
	ExpiresIn   int64     `json:"expires_in"`
	IssuedAt    time.Time `json:"issued_at"`
}

type UserContext struct {
	Subject string
	Groups  []string
}

type Authenticator interface {
	RequirePull(ctx context.Context, mirrorRepo string) (*UserContext, error)
}

type PullAuthorizer interface {
	AuthorizePull(principal authx.Principal, resource string) authx.Decision
}
