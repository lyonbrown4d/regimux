package auth

import (
	"crypto/subtle"
	"strings"

	"github.com/arcgolabs/authx"
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func (s *Service) serviceName() string {
	if s == nil || strings.TrimSpace(s.auth.Service) == "" {
		return "regimux"
	}
	return strings.TrimSpace(s.auth.Service)
}

func (s *Service) issuer() string {
	if s == nil || strings.TrimSpace(s.auth.Issuer) == "" {
		return s.serviceName()
	}
	return strings.TrimSpace(s.auth.Issuer)
}

func verifyPassword(user config.AuthUserConfig, password string) error {
	if hash := strings.TrimSpace(user.PasswordHash); hash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
			return newAuthError(authx.ErrorCodeUnauthenticated, "registry credentials are invalid")
		}
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(user.Password), []byte(password)) != 1 {
		return newAuthError(authx.ErrorCodeUnauthenticated, "registry credentials are invalid")
	}
	return nil
}

func principalListValues(values *collectionlist.List[string]) []string {
	if values == nil {
		return nil
	}
	return values.Values()
}
