package reference

import (
	"errors"
	"strings"

	ocidigest "github.com/opencontainers/go-digest"
	"github.com/samber/oops"
)

var (
	ErrDigestInvalid = errors.New("invalid digest")
)

// NormalizeDigest validates digest syntax and returns the canonical form.
func NormalizeDigest(value string) (string, error) {
	value, err := cleanDigestValue(value)
	if err != nil {
		return "", err
	}

	algorithm, encoded, _ := strings.Cut(value, ":")
	algorithm = strings.ToLower(algorithm)
	encoded = strings.ToLower(encoded)
	digest, err := ocidigest.Parse(algorithm + ":" + encoded)
	if err != nil {
		return "", oops.Wrapf(ErrDigestInvalid, "%q", value)
	}

	if err := validateDigestAlgorithm(digest.Algorithm()); err != nil {
		return "", err
	}

	return digest.String(), nil
}

func cleanDigestValue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrDigestInvalid
	}
	if strings.ContainsAny(value, "/?#") {
		return "", oops.Wrapf(ErrDigestInvalid, "%q", value)
	}
	if !strings.Contains(value, ":") {
		return "", oops.Wrapf(ErrDigestInvalid, "%q", value)
	}
	return value, nil
}

func validateDigestAlgorithm(algorithm ocidigest.Algorithm) error {
	switch algorithm {
	case ocidigest.SHA256, ocidigest.SHA384, ocidigest.SHA512:
		return nil
	default:
		return oops.Wrapf(ErrDigestInvalid, "unsupported digest algorithm %q", algorithm)
	}
}

// ValidateDigest validates digest syntax.
func ValidateDigest(value string) error {
	_, err := NormalizeDigest(value)
	return err
}

// IsDigest reports whether value is a valid canonicalizable digest.
func IsDigest(value string) bool {
	return ValidateDigest(value) == nil
}
