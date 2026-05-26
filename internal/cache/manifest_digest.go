package cache

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
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
	return "sha256:" + digestHex("sha256", body), nil
}

func digestFromUpstreamHeader(upstreamDigest string, body []byte) (string, bool, error) {
	if strings.TrimSpace(upstreamDigest) == "" {
		return "", false, nil
	}
	normalized, normalizeErr := reference.NormalizeDigest(upstreamDigest)
	if normalizeErr != nil {
		return "", false, fmt.Errorf("normalize upstream manifest digest: %w", normalizeErr)
	}
	if err := verifyDigestBody(normalized, body); err != nil {
		return "", true, err
	}
	return normalized, true, nil
}

func digestFromReference(raw string, body []byte) (string, error) {
	digest, err := reference.NormalizeDigest(raw)
	if err != nil {
		return "", fmt.Errorf("normalize manifest reference digest: %w", err)
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
	actual, err := digestForBody(expected, body)
	if err != nil {
		return err
	}
	if actual != expected {
		return distribution.ErrDigestMismatch.WithDetail(map[string]string{
			"expected": expected,
			"actual":   actual,
		})
	}
	return nil
}

func digestForBody(expectedDigest string, body []byte) (string, error) {
	algorithm, _, _ := strings.Cut(expectedDigest, ":")
	encoded := digestHex(algorithm, body)
	if encoded == "" {
		return "", distribution.ErrDigestInvalid.WithDetail("unsupported digest algorithm: " + algorithm)
	}
	return algorithm + ":" + encoded, nil
}

func digestHex(algorithm string, body []byte) string {
	switch algorithm {
	case "sha256":
		sum := sha256.Sum256(body)
		return hex.EncodeToString(sum[:])
	case "sha384":
		sum := sha512.Sum384(body)
		return hex.EncodeToString(sum[:])
	case "sha512":
		sum := sha512.Sum512(body)
		return hex.EncodeToString(sum[:])
	default:
		return ""
	}
}
