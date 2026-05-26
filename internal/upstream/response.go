package upstream

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"resty.dev/v3"
)

type upstreamResponse struct {
	Body       io.ReadCloser
	Header     http.Header
	StatusCode int
}

func rawUpstreamResponse(resp *resty.Response) (upstreamResponse, error) {
	if resp == nil || resp.RawResponse == nil {
		return upstreamResponse{}, errors.New("upstream response is empty")
	}
	body := resp.RawResponse.Body
	if resp.Body != nil {
		body = resp.Body
	}
	return upstreamResponse{
		Body:       body,
		Header:     resp.RawResponse.Header,
		StatusCode: resp.RawResponse.StatusCode,
	}, nil
}

func closeBody(body io.Closer) error {
	if body == nil {
		return nil
	}
	if err := body.Close(); err != nil {
		return fmt.Errorf("close upstream response body: %w", err)
	}
	return nil
}

func closeBodyWithError(body io.Closer, err error) error {
	closeErr := closeBody(body)
	if closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}

func drainAndClose(body io.ReadCloser) error {
	if body == nil {
		return nil
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		return errors.Join(
			fmt.Errorf("drain upstream response body: %w", err),
			closeBody(body),
		)
	}
	return closeBody(body)
}

func contentType(header http.Header) string {
	value := header.Get("Content-Type")
	if value == "" {
		return "application/octet-stream"
	}
	if before, _, ok := strings.Cut(value, ";"); ok {
		return strings.TrimSpace(before)
	}
	return value
}

func contentLength(header http.Header) int64 {
	value := header.Get("Content-Length")
	if value == "" {
		return -1
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}
	return n
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
