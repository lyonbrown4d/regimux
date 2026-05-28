package registrytool_test

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/registrytool"
	"github.com/stretchr/testify/require"
)

func TestORASRepositoryUsesNormalizedRegistry(t *testing.T) {
	client := registrytool.NewClient()

	repo, err := client.ORASRepository(registrytool.RepositoryRef{
		Registry:   "http://registry.test:5000/",
		Repository: "library/node",
		PageSize:   50,
	})

	require.NoError(t, err)
	require.True(t, repo.PlainHTTP)
	require.Equal(t, 50, repo.TagListPageSize)
	require.Equal(t, "registry.test:5000", repo.Reference.Registry)
	require.Equal(t, "library/node", repo.Reference.Repository)
}
