package reference

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	ErrDigestInvalid = errors.New("invalid digest")
	digestRegexp     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:[+._-][A-Za-z][A-Za-z0-9]*)*:[A-Fa-f0-9]+$`)
)

// NormalizeDigest validates digest syntax and returns the canonical form.
func NormalizeDigest(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrDigestInvalid
	}
	if strings.ContainsAny(value, "/?#") {
		return "", fmt.Errorf("%w: %q", ErrDigestInvalid, value)
	}
	if !digestRegexp.MatchString(value) {
		return "", fmt.Errorf("%w: %q", ErrDigestInvalid, value)
	}

	algorithm, encoded, _ := strings.Cut(value, ":")
	algorithm = strings.ToLower(algorithm)
	encoded = strings.ToLower(encoded)

	switch algorithm {
	case "sha256":
		if len(encoded) != 64 {
			return "", fmt.Errorf("%w: sha256 digest must be 64 hex characters", ErrDigestInvalid)
		}
	case "sha384":
		if len(encoded) != 96 {
			return "", fmt.Errorf("%w: sha384 digest must be 96 hex characters", ErrDigestInvalid)
		}
	case "sha512":
		if len(encoded) != 128 {
			return "", fmt.Errorf("%w: sha512 digest must be 128 hex characters", ErrDigestInvalid)
		}
	default:
		return "", fmt.Errorf("%w: unsupported digest algorithm %q", ErrDigestInvalid, algorithm)
	}

	return algorithm + ":" + encoded, nil
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
