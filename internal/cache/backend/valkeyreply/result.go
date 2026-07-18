// Package valkeyreply decodes lock command replies returned by Valkey.
package valkeyreply

import (
	"errors"
	"fmt"
)

type Result interface {
	Error() error
	AsInt64() (int64, error)
	ToString() (string, error)
}

func ParseLock(result Result) (bool, error) {
	if err := result.Error(); err != nil {
		return false, fmt.Errorf("read valkey lock reply: %w", err)
	}

	value, integerErr := result.AsInt64()
	if integerErr == nil {
		return value == 1, nil
	}

	text, stringErr := result.ToString()
	if stringErr == nil {
		return text == "1", nil
	}

	return false, fmt.Errorf("parse valkey lock reply: %w", errors.Join(integerErr, stringErr))
}
