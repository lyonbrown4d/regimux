package cache

import (
	"strings"

	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
)

func manifestDigest(req ManifestRequest, upstreamDigest string, body []byte) (string, error) {
	if digest, ok, err := digestFromUpstreamHeader(upstreamDigest, body); ok || err != nil {
		return digest, err
	}
	if reference.IsDigest(req.Reference) {
		return digestFromReference(req.Reference, body)
	}
	if body == nil {
		return "", nil
	}
	return ocidigest.FromBytes(body).String(), nil
}

func digestFromUpstreamHeader(upstreamDigest string, body []byte) (string, bool, error) {
	if strings.TrimSpace(upstreamDigest) == "" {
		return "", false, nil
	}
	normalized, normalizeErr := reference.NormalizeDigest(upstreamDigest)
	if normalizeErr != nil {
		return "", false, wrapError(normalizeErr, "normalize upstream manifest digest")
	}
	if err := verifyDigestBody(normalized, body); err != nil {
		return "", true, err
	}
	return normalized, true, nil
}

func digestFromReference(raw string, body []byte) (string, error) {
	digest, err := reference.NormalizeDigest(raw)
	if err != nil {
		return "", wrapError(err, "normalize manifest reference digest")
	}
	if err := verifyDigestBody(digest, body); err != nil {
		return "", err
	}
	return digest, nil
}

func verifyDigestBody(expected string, body []byte) error {
	if body == nil {
		return nil
	}
	expectedDigest, err := ocidigest.Parse(expected)
	if err != nil {
		return distribution.ErrDigestInvalid.WithDetail("invalid digest: " + expected)
	}
	verifier := expectedDigest.Verifier()
	if _, err := verifier.Write(body); err != nil {
		return wrapError(err, "verify digest body")
	}
	if !verifier.Verified() {
		actual := expectedDigest.Algorithm().FromBytes(body).String()
		return distribution.ErrDigestMismatch.WithDetail(map[string]string{
			"expected": expected,
			"actual":   actual,
		})
	}
	return nil
}
