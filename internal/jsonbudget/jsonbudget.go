// Package jsonbudget checks JSON input against allocation-oriented shape limits.
package jsonbudget

import (
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// ErrLimit indicates that JSON input exceeds one of its configured shape limits.
var ErrLimit = errors.New("JSON input exceeds structural limits")

// ErrInvalidJSON indicates that input is not valid UTF-8 JSON.
var ErrInvalidJSON = errors.New("JSON input is invalid")

// Limits bounds structural allocation drivers before a caller decodes JSON.
type Limits struct {
	MaxStructuralMarkers int
	MaxDepth             int
	MaxStringBytes       int
	MaxLiteralBytes      int
}

// DefaultLimits returns conservative limits for general metadata documents.
func DefaultLimits() Limits {
	return Limits{
		MaxStructuralMarkers: 100_000,
		MaxDepth:             128,
		MaxStringBytes:       32 << 20,
		MaxLiteralBytes:      1 << 10,
	}
}

// Check validates JSON syntax and bounds its decoded-shape allocation drivers.
func Check(data []byte, limits Limits) error {
	if err := checkShape(data, limits); err != nil {
		return err
	}
	if !utf8.Valid(data) || !json.Valid(data) {
		return ErrInvalidJSON
	}
	return nil
}

func checkShape(data []byte, limits Limits) error {
	scanner := shapeScanner{limits: limits}
	for index := range data {
		if err := scanner.advance(data[index]); err != nil {
			return err
		}
	}
	return nil
}

type shapeScanner struct {
	limits       Limits
	markers      int
	depth        int
	stringBytes  int
	literalBytes int
	inString     bool
	escaped      bool
}

func (scanner *shapeScanner) advance(char byte) error {
	if scanner.inString {
		return scanner.consumeString(char)
	}
	if char == '"' {
		scanner.inString = true
		scanner.literalBytes = 0
		return nil
	}
	if depthChange, isMarker := structureMarker(char); isMarker {
		return scanner.consumeMarker(depthChange)
	}
	if isJSONWhitespace(char) {
		scanner.literalBytes = 0
		return nil
	}
	scanner.literalBytes++
	if scanner.literalBytes > scanner.limits.MaxLiteralBytes {
		return ErrLimit
	}
	return nil
}

func (scanner *shapeScanner) consumeString(char byte) error {
	scanner.stringBytes += countJSONStringByte(char, &scanner.inString, &scanner.escaped)
	if scanner.stringBytes > scanner.limits.MaxStringBytes {
		return ErrLimit
	}
	return nil
}

func (scanner *shapeScanner) consumeMarker(depthChange int) error {
	scanner.markers++
	scanner.depth += depthChange
	scanner.literalBytes = 0
	if scanner.markers > scanner.limits.MaxStructuralMarkers || scanner.depth > scanner.limits.MaxDepth {
		return ErrLimit
	}
	return nil
}

func countJSONStringByte(char byte, inString, escaped *bool) int {
	if *escaped {
		*escaped = false
		return 1
	}
	switch char {
	case '\\':
		*escaped = true
		return 1
	case '"':
		*inString = false
		return 0
	default:
		return 1
	}
}

func structureMarker(char byte) (int, bool) {
	switch char {
	case '{', '[':
		return 1, true
	case '}', ']':
		return -1, true
	case ',', ':':
		return 0, true
	default:
		return 0, false
	}
}

func isJSONWhitespace(char byte) bool {
	switch char {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}
