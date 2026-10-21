package npm

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

func encodedJSONSize(value any) (int64, error) {
	switch item := value.(type) {
	case map[string]any:
		if item == nil {
			return 4, nil
		}
		return encodedJSONObjectSize(item)
	case []any:
		if item == nil {
			return 4, nil
		}
		return encodedJSONArraySize(item)
	case string:
		return encodedJSONStringSize(item), nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return 0, fmt.Errorf("measure encoded JSON value: %w", err)
		}
		return int64(len(encoded)), nil
	}
}

func encodedJSONObjectSize(value map[string]any) (int64, error) {
	size := int64(2)
	first := true
	for key, item := range value {
		if !first {
			size++
		}
		first = false
		valueSize, err := encodedJSONSize(item)
		if err != nil {
			return 0, err
		}
		size += encodedJSONStringSize(key) + 1 + valueSize
	}
	return size, nil
}

func encodedJSONArraySize(value []any) (int64, error) {
	size := int64(2)
	for index, item := range value {
		if index > 0 {
			size++
		}
		itemSize, err := encodedJSONSize(item)
		if err != nil {
			return 0, err
		}
		size += itemSize
	}
	return size, nil
}

func encodedJSONStringSize(value string) int64 {
	size := int64(2)
	for value != "" {
		r, width := utf8.DecodeRuneInString(value)
		value = value[width:]
		size += encodedJSONRuneSize(r, width)
	}
	return size
}

func encodedJSONRuneSize(r rune, width int) int64 {
	if r == utf8.RuneError && width == 1 {
		return int64(utf8.RuneLen(utf8.RuneError))
	}
	switch r {
	case '"', '\\', '\b', '\f', '\n', '\r', '\t':
		return 2
	case '<', '>', '&', '\u2028', '\u2029':
		return 6
	}
	if r < 0x20 {
		return 6
	}
	return int64(width)
}
