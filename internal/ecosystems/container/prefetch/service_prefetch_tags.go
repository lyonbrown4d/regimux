package prefetch

import (
	"context"
	"encoding/json"
	"strconv"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
)

func (s *Service) availableTags(ctx context.Context, route repoKey, pageSize int) (*collectionlist.List[string], error) {
	result, err := s.tags.List(ctx, cache.TagRequest{
		UpstreamAlias: route.alias,
		Repo:          route.repo,
		N:             strconv.Itoa(pageSize),
	})
	if err != nil {
		return nil, cacheWrap(err, "list tags for prefetch")
	}
	var body struct {
		Tags []string `json:"tags"`
	}
	if err := jsonbudget.Check(result.Body, jsonbudget.DefaultLimits()); err != nil {
		return nil, cacheWrap(err, "check tags response JSON budget for prefetch")
	}
	if err := json.Unmarshal(result.Body, &body); err != nil {
		return nil, cacheWrap(err, "decode tags response for prefetch")
	}
	return collectionlist.NewList(body.Tags...), nil
}
