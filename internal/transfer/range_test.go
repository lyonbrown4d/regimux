package transfer_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/transfer"
	"github.com/stretchr/testify/require"
)

func TestApplyRangeSemantics(t *testing.T) {
	for _, test := range []struct {
		name, method, requested, validator, body, contentRange string
		status                                                 int
	}{
		{name: "bounded", method: "GET", requested: "bytes=2-5", body: "2345", contentRange: "bytes 2-5/10", status: 206},
		{name: "suffix", method: "GET", requested: "bytes=-4", body: "6789", contentRange: "bytes 6-9/10", status: 206},
		{name: "open", method: "GET", requested: "bytes=8-", body: "89", contentRange: "bytes 8-9/10", status: 206},
		{name: "match", method: "GET", requested: "bytes=-4", validator: `"fixed"`, body: "6789", contentRange: "bytes 6-9/10", status: 206},
		{name: "changed", method: "GET", requested: "bytes=-4", validator: `"changed"`, body: "0123456789", status: 200},
		{name: "weak", method: "GET", requested: "bytes=-4", validator: `W/"fixed"`, body: "0123456789", status: 200},
		{name: "head", method: "HEAD", requested: "bytes=999-", body: "0123456789", status: 200},
		{name: "invalid", method: "GET", requested: "bytes=999-", contentRange: "bytes */10", status: 416},
	} {
		t.Run(test.name, func(t *testing.T) {
			rep, err := transfer.ApplyRange(test.method, test.requested, test.validator, transfer.Response{
				Status: http.StatusOK, Headers: http.Header{"Etag": {`"fixed"`}}, Size: 10,
				Body: io.NopCloser(strings.NewReader("0123456789")),
			})
			require.NoError(t, err)
			body, err := io.ReadAll(rep.Body)
			require.NoError(t, err)
			require.Equal(t, test.status, rep.Status)
			require.Equal(t, test.body, string(body))
			require.Equal(t, test.contentRange, rep.Headers.Get("Content-Range"))
			require.Equal(t, "bytes", rep.Headers.Get("Accept-Ranges"))
			require.NoError(t, rep.Body.Close())
		})
	}
}

func TestApplyRangeUsesNativeOpenerAndClosesFullBody(t *testing.T) {
	full := &countedBody{Reader: strings.NewReader("0123456789")}
	rep, err := transfer.ApplyRange(http.MethodGet, "bytes=-3", "", transfer.Response{
		Status: http.StatusOK, Size: 10, Body: full,
	}, func(start, length int64) (io.ReadCloser, error) {
		if start != 7 || length != 3 || full.closes.Load() != 1 {
			t.Errorf("start=%d length=%d full-body closes=%d", start, length, full.closes.Load())
		}
		return io.NopCloser(strings.NewReader("789")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(rep.Body)
	if err != nil || string(body) != "789" {
		t.Fatalf("body=%q error=%v", body, err)
	}
	if err := rep.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRangeRejectsInvalidInitialPartialResponses(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		rangeValue string
		ifRange    string
		headers    http.Header
	}{
		{
			name:       "wrong start",
			method:     http.MethodGet,
			rangeValue: "bytes=8-11",
			headers:    http.Header{"Content-Range": {"bytes 0-3/16"}},
		},
		{
			name:       "wrong end",
			method:     http.MethodGet,
			rangeValue: "bytes=8-11",
			headers:    http.Header{"Content-Range": {"bytes 8-12/16"}},
		},
		{
			name:       "unknown total cannot resolve suffix",
			method:     http.MethodGet,
			rangeValue: "bytes=-4",
			headers:    http.Header{"Content-Range": {"bytes 8-11/*"}},
		},
		{
			name:       "if-range mismatch",
			method:     http.MethodGet,
			rangeValue: "bytes=8-11",
			ifRange:    `"old"`,
			headers: http.Header{
				"Content-Range": {"bytes 8-11/16"},
				"ETag":          {`"new"`},
			},
		},
		{
			name:       "if-range validator missing",
			method:     http.MethodGet,
			rangeValue: "bytes=8-11",
			ifRange:    `"old"`,
			headers:    http.Header{"Content-Range": {"bytes 8-11/16"}},
		},
		{
			name:    "unsolicited partial response",
			method:  http.MethodHead,
			headers: http.Header{"Content-Range": {"bytes 0-3/16"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := &trackedRangeBody{Reader: strings.NewReader("data")}
			_, err := transfer.ApplyRange(test.method, test.rangeValue, test.ifRange, transfer.Response{
				Status: http.StatusPartialContent, Headers: test.headers, Body: body, Size: 4,
			})
			require.Error(t, err)
			require.True(t, body.closed, "rejected response body must be closed")
		})
	}
}

func TestApplyRangeAcceptsValidPartialWithUnknownTotal(t *testing.T) {
	body := &trackedRangeBody{Reader: strings.NewReader("34")}
	response, err := transfer.ApplyRange(http.MethodGet, "bytes=2-5", "", transfer.Response{
		Status: http.StatusPartialContent,
		Headers: http.Header{
			"Content-Range":  {"bytes 3-4/*"},
			"Content-Length": {"2"},
		},
		Body: body,
		Size: 2,
	})
	require.NoError(t, err)
	got, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "34", string(got))
	require.Equal(t, "bytes 3-4/*", response.Headers.Get("Content-Range"))
	require.NoError(t, response.Body.Close())
}

type trackedRangeBody struct {
	io.Reader
	closed bool
}

func (b *trackedRangeBody) Close() error {
	b.closed = true
	return nil
}
