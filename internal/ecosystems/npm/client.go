package npm

import (
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func (s *Service) clientFor(cfg config.UpstreamConfig, baseURL string) (*http.Client, error) {
	if s.client != nil {
		return s.client, nil
	}
	factory := s.factory
	if factory == nil {
		factory = clientfactory.New(s.logger)
	}
	client, err := factory.RawUpstreamHTTP(cfg, baseURL, "npm.clientx")
	if err != nil {
		return nil, wrapError(err, "create npm upstream client")
	}
	return client, nil
}
