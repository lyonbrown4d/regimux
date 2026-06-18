package npm

import (
	clienthttp "github.com/arcgolabs/clientx/http"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

func (s *Service) clientFor(cfg config.UpstreamConfig, baseURL string) (clienthttp.Client, error) {
	factory := s.factory
	client, err := upstreamhttp.NewClient(factory, cfg, baseURL, "npm.clientx")
	if err != nil {
		return nil, wrapError(err, "create npm upstream client")
	}
	return client, nil
}
