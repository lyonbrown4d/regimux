package meta

import (
	"errors"
	"strings"

	ocidigest "github.com/opencontainers/go-digest"
	"github.com/samber/oops"
)

var (
	errDigestInvalid = errors.New("invalid digest")
)

func normalizeDigestValue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errDigestInvalid
	}
	if strings.ContainsAny(value, "/?#") {
		return "", oops.Wrapf(errDigestInvalid, "%q", value)
	}
	if !strings.Contains(value, ":") {
		return "", oops.Wrapf(errDigestInvalid, "%q", value)
	}

	algorithm, encoded, _ := strings.Cut(value, ":")
	digest, err := ocidigest.Parse(strings.ToLower(algorithm) + ":" + strings.ToLower(encoded))
	if err != nil {
		return "", oops.Wrapf(errDigestInvalid, "%q", value)
	}
	if err := validateDigestAlgorithm(digest.Algorithm()); err != nil {
		return "", err
	}
	return digest.String(), nil
}

func validateDigestAlgorithm(algorithm ocidigest.Algorithm) error {
	switch algorithm {
	case ocidigest.SHA256, ocidigest.SHA384, ocidigest.SHA512:
		return nil
	default:
		return oops.Wrapf(errDigestInvalid, "unsupported digest algorithm %q", algorithm)
	}
}
