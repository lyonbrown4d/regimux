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
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, nil
	}
	if !strings.HasPrefix(strings.ToLower(header), "bytes=") {
		return nil, fmt.Errorf("%w: only bytes ranges are supported", errRangeInvalid)
	}

	spec := strings.TrimSpace(header[len("bytes="):])
	if spec == "" || strings.Contains(spec, ",") {
		return nil, fmt.Errorf("%w: only a single bytes range is supported", errRangeInvalid)
	}

	left, right, ok := strings.Cut(spec, "-")
	if !ok {
		return nil, fmt.Errorf("%w: missing dash", errRangeInvalid)
	}
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)

	switch {
	case left == "" && right == "":
		return nil, fmt.Errorf("%w: empty range", errRangeInvalid)
	case left == "":
		suffix, err := parseNonNegativeInt(right)
		if err != nil || suffix <= 0 {
			return nil, fmt.Errorf("%w: invalid suffix length", errRangeInvalid)
		}
		return &HTTPRange{Start: -1, End: suffix}, nil
	case right == "":
		start, err := parseNonNegativeInt(left)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid start", errRangeInvalid)
		}
		return &HTTPRange{Start: start, End: -1}, nil
	default:
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
	if size < 0 {
		return nil, fmt.Errorf("%w: negative size", errRangeInvalid)
	}
	if size == 0 {
		return nil, fmt.Errorf("%w: empty content", errRangeInvalid)
	}

	switch {
	case r.IsSuffix():
		length := r.End
		if length > size {
			length = size
		}
		return &HTTPRange{Start: size - length, End: size - 1}, nil
	case r.IsOpenEnded():
		if r.Start >= size {
			return nil, fmt.Errorf("%w: start beyond content size", errRangeInvalid)
		}
		return &HTTPRange{Start: r.Start, End: size - 1}, nil
	default:
		if r.Start < 0 || r.End < r.Start || r.Start >= size {
			return nil, fmt.Errorf("%w: unsatisfiable range", errRangeInvalid)
		}
		end := r.End
		if end >= size {
			end = size - 1
		}
		return &HTTPRange{Start: r.Start, End: end}, nil
	}
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
