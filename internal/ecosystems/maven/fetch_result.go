package maven

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/config"
)

// Missing artifacts are local to an equivalent mirror, unlike authentication
// failures. Keep misses separate so they cannot conceal an unavailable replica.
type mavenEndpointResult struct {
	missing   *upstreamFetch
	retryable *upstreamFetch
	err       error
}

func (state *mavenEndpointResult) remember(fetched *upstreamFetch, err error, logger *slog.Logger) bool {
	if err != nil {
		state.err = err
		return true
	}
	var previous **upstreamFetch
	switch {
	case fetched.status == http.StatusNotFound:
		previous = &state.missing
	case retryableMavenStatus(fetched.status):
		previous = &state.retryable
	default:
		return false
	}
	if *previous != nil {
		closeReadCloser((*previous).body, logger, "close superseded maven endpoint response body")
	}
	*previous = fetched
	return true
}

func (state *mavenEndpointResult) take(cfg config.UpstreamConfig, attempted []string) (*upstreamFetch, error) {
	// Preserve the existing retryable-response precedence over transport errors.
	if state.retryable != nil {
		fetched := state.retryable
		state.retryable = nil
		return fetched, nil
	}
	if state.err != nil {
		return nil, wrapError(state.err, "fetch maven upstream")
	}
	if state.missing != nil {
		if hasUntriedMavenEndpoint(cfg, attempted) {
			return nil, wrapError(
				errors.New("some maven mirrors were not checked because they are unavailable"),
				"fetch maven upstream",
			)
		}
		fetched := state.missing
		state.missing = nil
		return fetched, nil
	}
	return nil, wrapError(errors.New("no maven upstream endpoint is configured"), "fetch maven upstream")
}

func (state *mavenEndpointResult) close(logger *slog.Logger) {
	for _, fetched := range []*upstreamFetch{state.missing, state.retryable} {
		if fetched != nil {
			closeReadCloser(fetched.body, logger, "close unused maven endpoint response body")
		}
	}
}

func hasUntriedMavenEndpoint(cfg config.UpstreamConfig, attempted []string) bool {
	// Health selection may exclude a configured replica. Respect its cooldown,
	// but do not turn an unchecked replica into an authoritative artifact miss.
	for _, endpoint := range append([]string{cfg.Registry}, cfg.Mirrors...) {
		endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
		if endpoint != "" && !slices.Contains(attempted, endpoint) {
			return true
		}
	}
	return false
}
