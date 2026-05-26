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
	value, err := cleanDigestValue(value)
	if err != nil {
		return "", err
	}

	algorithm, encoded, _ := strings.Cut(value, ":")
	algorithm = strings.ToLower(algorithm)
	encoded = strings.ToLower(encoded)

	if err := validateDigestLength(algorithm, encoded); err != nil {
		return "", err
	}

	return algorithm + ":" + encoded, nil
}

func cleanDigestValue(value string) (string, error) {
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
	return value, nil
}

func validateDigestLength(algorithm, encoded string) error {
	length, ok := digestLength(algorithm)
	if !ok {
		return fmt.Errorf("%w: unsupported digest algorithm %q", ErrDigestInvalid, algorithm)
	}
	if len(encoded) != length {
		return fmt.Errorf("%w: %s digest must be %d hex characters", ErrDigestInvalid, algorithm, length)
	}
	return nil
}

func digestLength(algorithm string) (int, bool) {
	switch algorithm {
	case "sha256":
		return 64, true
	case "sha384":
		return 96, true
	case "sha512":
		return 128, true
	default:
		return 0, false
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
