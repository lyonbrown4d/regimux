package reference

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var errRangeInvalid = errors.New("invalid range")

// HTTPRange represents a single bytes range from an HTTP Range header.
// End is inclusive. For open-ended ranges End is -1. For suffix ranges
// Start is -1 and End contains the suffix length.
type HTTPRange struct {
	Start int64
	End   int64
}

// ParseRange parses an HTTP Range header. Empty headers return nil, nil.
func ParseRange(header string) (*HTTPRange, error) {
	spec, ok, err := rangeSpec(header)
	if err != nil {
		return nil, err
	}
	if !ok {
		var noRange *HTTPRange
		return noRange, nil
	}

	left, right, err := splitRangeSpec(spec)
	if err != nil {
		return nil, err
	}
	return parseRangeBounds(left, right)
}

func rangeSpec(header string) (string, bool, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", false, nil
	}
	if !strings.HasPrefix(strings.ToLower(header), "bytes=") {
		return "", false, fmt.Errorf("%w: only bytes ranges are supported", errRangeInvalid)
	}

	spec := strings.TrimSpace(header[len("bytes="):])
	if spec == "" || strings.Contains(spec, ",") {
		return "", false, fmt.Errorf("%w: only a single bytes range is supported", errRangeInvalid)
	}
	return spec, true, nil
}

func splitRangeSpec(spec string) (string, string, error) {
	left, right, ok := strings.Cut(spec, "-")
	if !ok {
		return "", "", fmt.Errorf("%w: missing dash", errRangeInvalid)
	}
	return strings.TrimSpace(left), strings.TrimSpace(right), nil
}

func parseRangeBounds(left, right string) (*HTTPRange, error) {
	switch {
	case left == "" && right == "":
		return nil, fmt.Errorf("%w: empty range", errRangeInvalid)
	case left == "":
		return parseSuffixRange(right)
	case right == "":
		return parseOpenEndedRange(left)
	default:
		return parseBoundedRange(left, right)
	}
}

func parseSuffixRange(right string) (*HTTPRange, error) {
	suffix, err := parseNonNegativeInt(right)
	if err != nil || suffix <= 0 {
		return nil, fmt.Errorf("%w: invalid suffix length", errRangeInvalid)
	}
	return &HTTPRange{Start: -1, End: suffix}, nil
}

func parseOpenEndedRange(left string) (*HTTPRange, error) {
	start, err := parseNonNegativeInt(left)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid start", errRangeInvalid)
	}
	return &HTTPRange{Start: start, End: -1}, nil
}

func parseBoundedRange(left, right string) (*HTTPRange, error) {
	start, err := parseNonNegativeInt(left)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid start", errRangeInvalid)
	}
	end, err := parseNonNegativeInt(right)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid end", errRangeInvalid)
	}
	if end < start {
		return nil, fmt.Errorf("%w: end before start", errRangeInvalid)
	}
	return &HTTPRange{Start: start, End: end}, nil
}

func parseNonNegativeInt(value string) (int64, error) {
	if value == "" || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return 0, errRangeInvalid
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return 0, errRangeInvalid
	}
	return n, nil
}

// IsSuffix reports whether r is a suffix bytes range such as bytes=-500.
func (r HTTPRange) IsSuffix() bool {
	return r.Start < 0 && r.End > 0
}

// IsOpenEnded reports whether r is an open-ended bytes range such as bytes=500-.
func (r HTTPRange) IsOpenEnded() bool {
	return r.Start >= 0 && r.End < 0
}

func (r HTTPRange) String() string {
	switch {
	case r.IsSuffix():
		return fmt.Sprintf("bytes=-%d", r.End)
	case r.IsOpenEnded():
		return fmt.Sprintf("bytes=%d-", r.Start)
	default:
		return fmt.Sprintf("bytes=%d-%d", r.Start, r.End)
	}
}

// Resolve converts suffix/open-ended ranges into an inclusive concrete range.
func (r HTTPRange) Resolve(size int64) (*HTTPRange, error) {
	if err := validateContentSize(size); err != nil {
		return nil, err
	}

	switch {
	case r.IsSuffix():
		return r.resolveSuffix(size), nil
	case r.IsOpenEnded():
		return r.resolveOpenEnded(size)
	default:
		return r.resolveBounded(size)
	}
}

func validateContentSize(size int64) error {
	if size < 0 {
		return fmt.Errorf("%w: negative size", errRangeInvalid)
	}
	if size == 0 {
		return fmt.Errorf("%w: empty content", errRangeInvalid)
	}
	return nil
}

func (r HTTPRange) resolveSuffix(size int64) *HTTPRange {
	length := min(r.End, size)
	return &HTTPRange{Start: size - length, End: size - 1}
}

func (r HTTPRange) resolveOpenEnded(size int64) (*HTTPRange, error) {
	if r.Start >= size {
		return nil, fmt.Errorf("%w: start beyond content size", errRangeInvalid)
	}
	return &HTTPRange{Start: r.Start, End: size - 1}, nil
}

func (r HTTPRange) resolveBounded(size int64) (*HTTPRange, error) {
	if r.Start < 0 || r.End < r.Start || r.Start >= size {
		return nil, fmt.Errorf("%w: unsatisfiable range", errRangeInvalid)
	}
	end := min(r.End, size-1)
	return &HTTPRange{Start: r.Start, End: end}, nil
}

// Length returns the inclusive byte length for a concrete range.
func (r HTTPRange) Length() int64 {
	if r.Start < 0 || r.End < r.Start {
		return 0
	}
	return r.End - r.Start + 1
}

// ContentRange returns the HTTP Content-Range header value for a concrete range.
func (r HTTPRange) ContentRange(size int64) string {
	return fmt.Sprintf("bytes %d-%d/%d", r.Start, r.End, size)
}
