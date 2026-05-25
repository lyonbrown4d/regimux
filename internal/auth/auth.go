package auth

import "context"

type UserContext struct {
	Subject string
	Groups  []string
}

type Authenticator interface {
	RequirePull(ctx context.Context, mirrorRepo string) (*UserContext, error)
}

type AllowAll struct{}

func (AllowAll) RequirePull(context.Context, string) (*UserContext, error) {
	return &UserContext{Subject: "anonymous"}, nil
}

