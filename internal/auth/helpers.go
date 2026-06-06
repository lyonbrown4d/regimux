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
	if s == nil {
		return registryAuthServiceName(config.RegistryAuthConfig{})
	}
	return s.serviceNameValue
}

func (s *Service) issuer() string {
	if s == nil {
		return registryAuthIssuer(config.RegistryAuthConfig{})
	}
	return s.issuerValue
}

func registryAuthServiceName(auth config.RegistryAuthConfig) string {
	service := strings.TrimSpace(auth.Service)
	if service == "" {
		return "regimux"
	}
	return service
}

func registryAuthIssuer(auth config.RegistryAuthConfig) string {
	issuer := strings.TrimSpace(auth.Issuer)
	if issuer == "" {
		return registryAuthServiceName(auth)
	}
	return issuer
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
