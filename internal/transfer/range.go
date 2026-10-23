package transfer

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type Response struct {
	Status  int
	Headers http.Header
	Body    io.ReadCloser
	Size    int64
}

func MatchesIfRange(value string, headers http.Header) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, "W/") {
		return false
	}
	if strings.HasPrefix(value, `"`) {
		return value == headers.Get("ETag")
	}
	modified, err := http.ParseTime(headers.Get("Last-Modified"))
	when, whenErr := http.ParseTime(value)
	return err == nil && whenErr == nil && modified.Equal(when)
}

// ApplyRange selects a single byte range from a complete representation. The
// optional opener performs native object range reads; otherwise seek or bounded
// discard is used. Ownership of the input body transfers to the returned body.
func ApplyRange(
	method, rangeHeader, ifRange string,
	rep Response,
	openers ...func(start, length int64) (io.ReadCloser, error),
) (Response, error) {
	if rep.Status == http.StatusPartialContent {
		return selectPartialResponse(method, rangeHeader, ifRange, rep)
	}
	if rep.Status != http.StatusOK || rep.Size < 0 {
		return rep, nil
	}
	rep.Headers = rep.Headers.Clone()
	if rep.Headers == nil {
		rep.Headers = make(http.Header)
	}
	rep.Headers.Set("Accept-Ranges", "bytes")
	if method != http.MethodGet || rangeHeader == "" || !MatchesIfRange(ifRange, rep.Headers) {
		return rep, nil
	}
	resolved, valid := resolveRequestedRange(rangeHeader, rep.Size)
	if !valid {
		return rejectedRange(rep)
	}
	body, err := rangeBody(rep.Body, resolved.Start, resolved.Length(), openers)
	if err != nil {
		return Response{}, err
	}
	rep.Body = body
	rep.Headers.Set("Content-Range", resolved.ContentRange(rep.Size))
	rep.Size = resolved.Length()
	rep.Status = http.StatusPartialContent
	rep.Headers.Set("Content-Length", strconv.FormatInt(rep.Size, 10))
	return rep, nil
}

func selectPartialResponse(method, rangeHeader, ifRange string, response Response) (Response, error) {
	if err := validatePartialResponse(method, rangeHeader, ifRange, response); err != nil {
		return closeInvalidPartialResponse(response, err)
	}
	return response, nil
}

func closeInvalidPartialResponse(response Response, validationErr error) (Response, error) {
	if response.Body != nil {
		validationErr = errors.Join(validationErr, response.Body.Close())
	}
	return Response{}, fmt.Errorf("validate partial artifact response: %w", validationErr)
}

func validatePartialResponse(method, rangeHeader, ifRange string, response Response) error {
	if method != http.MethodGet {
		return errors.New("partial artifact response is unexpected for this method")
	}
	requested, err := object.ParseRange(rangeHeader)
	if err != nil || requested == nil {
		return errors.New("partial artifact response has no valid requested range")
	}
	if !MatchesIfRange(ifRange, response.Headers) {
		return errors.New("partial artifact response does not match If-Range")
	}
	actual, total, known, err := parsePartialContentRange(response.Headers.Get("Content-Range"))
	if err != nil {
		return err
	}
	if actual.Length() <= 0 {
		return errors.New("partial artifact response has an invalid interval length")
	}
	if response.Body == nil || response.Body == http.NoBody {
		return errors.New("partial artifact response has no body")
	}
	if err := validatePartialContentLength(response.Headers, actual.Length()); err != nil {
		return err
	}
	return validateRequestedPartialRange(*requested, actual, total, known)
}

func validateRequestedPartialRange(requested, actual object.HTTPRange, total int64, totalKnown bool) error {
	if totalKnown {
		resolved, err := requested.Resolve(total)
		if err != nil || actual.Start < resolved.Start || actual.End > resolved.End {
			return errors.New("partial artifact response is outside the requested range")
		}
		return nil
	}
	if requested.IsSuffix() {
		return errors.New("partial artifact response cannot validate a suffix range with unknown total")
	}
	if actual.Start < requested.Start || (!requested.IsOpenEnded() && actual.End > requested.End) {
		return errors.New("partial artifact response is outside the requested range")
	}
	return nil
}

func parsePartialContentRange(value string) (object.HTTPRange, int64, bool, error) {
	interval, totalValue, err := splitPartialContentRange(value)
	if err != nil {
		return object.HTTPRange{}, 0, false, err
	}
	actual, err := parsePartialRangeInterval(interval)
	if err != nil {
		return object.HTTPRange{}, 0, false, err
	}
	if totalValue == "*" {
		return actual, 0, false, nil
	}
	total, err := parseRangeTotal(totalValue)
	if err != nil || total <= actual.End {
		return object.HTTPRange{}, 0, false, errors.New("partial artifact response has an invalid Content-Range total")
	}
	return actual, total, true, nil
}

func splitPartialContentRange(value string) (string, string, error) {
	unit, value, ok := strings.Cut(strings.TrimSpace(value), " ")
	if !ok || !strings.EqualFold(unit, "bytes") {
		return "", "", errors.New("partial artifact response has an invalid Content-Range")
	}
	interval, totalValue, ok := strings.Cut(strings.TrimSpace(value), "/")
	if !ok {
		return "", "", errors.New("partial artifact response has an invalid Content-Range")
	}
	return strings.TrimSpace(interval), strings.TrimSpace(totalValue), nil
}

func parsePartialRangeInterval(interval string) (object.HTTPRange, error) {
	actual, err := object.ParseRange("bytes=" + interval)
	if err != nil || actual == nil || actual.IsOpenEnded() || actual.IsSuffix() {
		return object.HTTPRange{}, errors.New("partial artifact response has an invalid Content-Range interval")
	}
	return *actual, nil
}

func parseRangeTotal(value string) (int64, error) {
	if value == "" {
		return 0, errors.New("empty Content-Range total")
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, errors.New("invalid Content-Range total")
		}
	}
	total, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse Content-Range total: %w", err)
	}
	return total, nil
}

func validatePartialContentLength(headers http.Header, expected int64) error {
	value := headers.Get("Content-Length")
	if value == "" {
		return nil
	}
	length, err := strconv.ParseInt(value, 10, 64)
	if err != nil || length != expected {
		return errors.New("partial artifact response has an inconsistent Content-Length")
	}
	return nil
}

func resolveRequestedRange(header string, size int64) (*object.HTTPRange, bool) {
	requested, err := object.ParseRange(header)
	if err != nil || requested == nil {
		return nil, false
	}
	resolved, err := requested.Resolve(size)
	return resolved, err == nil
}

func rejectedRange(rep Response) (Response, error) {
	if rep.Body != nil {
		if err := rep.Body.Close(); err != nil {
			return Response{}, fmt.Errorf("close unsatisfiable range: %w", err)
		}
	}
	rep.Headers.Set("Content-Range", fmt.Sprintf("bytes */%d", rep.Size))
	rep.Headers.Set("Content-Length", "0")
	rep.Status, rep.Size, rep.Body = http.StatusRequestedRangeNotSatisfiable, 0, http.NoBody
	return rep, nil
}

func rangeBody(body io.ReadCloser, start, length int64, openers []func(int64, int64) (io.ReadCloser, error)) (io.ReadCloser, error) {
	if len(openers) > 0 && openers[0] != nil {
		if err := body.Close(); err != nil {
			return nil, fmt.Errorf("close full artifact before range read: %w", err)
		}
		reader, err := openers[0](start, length)
		if err != nil {
			return nil, fmt.Errorf("open artifact range: %w", err)
		}
		return reader, nil
	}
	var err error
	if seeker, ok := body.(io.Seeker); ok {
		_, err = seeker.Seek(start, io.SeekStart)
	} else {
		_, err = io.CopyN(io.Discard, body, start)
	}
	if err != nil {
		return nil, fmt.Errorf("position artifact range: %w", errors.Join(err, body.Close()))
	}
	return &limitedBody{Reader: io.LimitReader(body, length), Closer: body}, nil
}

type limitedBody struct {
	io.Reader
	io.Closer
}
