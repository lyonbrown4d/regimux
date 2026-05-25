package cache

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

type manifestEnvelope struct {
	Record meta.ManifestRecord `json:"record"`
	Body   []byte              `json:"body,omitempty"`
}

type responseEnvelope struct {
	Body      []byte              `json:"body,omitempty"`
	Headers   map[string][]string `json:"headers,omitempty"`
	MediaType string              `json:"media_type,omitempty"`
}

func manifestCacheKey(req ManifestRequest) string {
	return strings.Join([]string{
		"manifest",
		req.UpstreamAlias,
		req.Repo,
		req.Reference,
		reference.AcceptKey(req.Accept),
	}, ":")
}

func tagsCacheKey(req TagRequest) string {
	return strings.Join([]string{
		"tags",
		req.UpstreamAlias,
		req.Repo,
		req.N,
		req.Last,
	}, ":")
}

func referrersCacheKey(req ReferrerRequest) string {
	return strings.Join([]string{
		"referrers",
		req.UpstreamAlias,
		req.Repo,
		req.Digest,
	}, ":")
}

func manifestEnvelopeFromRecord(record meta.ManifestRecord, body []byte) ([]byte, error) {
	return json.Marshal(manifestEnvelope{
		Record: record,
		Body:   body,
	})
}

func manifestFromEnvelope(data []byte) (*CachedManifest, error) {
	var envelope manifestEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	return &CachedManifest{
		Digest:    envelope.Record.Digest,
		MediaType: envelope.Record.MediaType,
		Size:      envelope.Record.Size,
		Body:      envelope.Body,
		Headers:   http.Header(envelope.Record.Headers).Clone(),
		Cache:     CacheHit,
	}, nil
}

func tagsEnvelopeFromResult(result *TagsResult) ([]byte, error) {
	if result == nil {
		return json.Marshal(responseEnvelope{})
	}
	return json.Marshal(responseEnvelope{
		Body:    result.Body,
		Headers: map[string][]string(result.Headers.Clone()),
	})
}

func tagsFromEnvelope(data []byte) (*TagsResult, error) {
	var envelope responseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	return &TagsResult{
		Body:    envelope.Body,
		Headers: http.Header(envelope.Headers).Clone(),
		Cache:   CacheHit,
	}, nil
}

func referrersEnvelopeFromResult(result *ReferrersResult) ([]byte, error) {
	if result == nil {
		return json.Marshal(responseEnvelope{})
	}
	return json.Marshal(responseEnvelope{
		Body:      result.Body,
		Headers:   map[string][]string(result.Headers.Clone()),
		MediaType: result.MediaType,
	})
}

func referrersFromEnvelope(data []byte) (*ReferrersResult, error) {
	var envelope responseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	return &ReferrersResult{
		Body:      envelope.Body,
		MediaType: envelope.MediaType,
		Headers:   http.Header(envelope.Headers).Clone(),
		Cache:     CacheHit,
	}, nil
}

func ttlUntil(expiresAt time.Time, fallback time.Duration) time.Duration {
	if expiresAt.IsZero() {
		return fallback
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return 0
	}
	return ttl
}
