package auth

import (
	"github.com/arcgolabs/authx"
	collectionlist "github.com/arcgolabs/collectionx/list"
)

func authenticationProviders(providers *collectionlist.List[authx.AuthenticationProvider]) *collectionlist.List[authx.AuthenticationProvider] {
	if providers == nil || providers.Len() == 0 {
		return collectionlist.NewList[authx.AuthenticationProvider]()
	}
	return providers
}
