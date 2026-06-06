package auth

import (
	"github.com/arcgolabs/authx"
	collectionlist "github.com/arcgolabs/collectionx/list"
)

func (s *Service) authenticationProviders() *collectionlist.List[authx.AuthenticationProvider] {
	if s.providers == nil || s.providers.Len() == 0 {
		return collectionlist.NewList[authx.AuthenticationProvider]()
	}
	return s.providers
}
