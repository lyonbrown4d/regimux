package artifactcache_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
)

func TestValidateBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		headers http.Header
		wantErr bool
	}{
		{name: "rejects empty body", wantErr: true},
		{
			name:    "rejects declared length mismatch",
			body:    "short",
			headers: http.Header{"Content-Length": []string{"10"}},
			wantErr: true,
		},
		{
			name:    "accepts matching declared length",
			body:    "body",
			headers: http.Header{"Content-Length": []string{"4"}},
		},
		{name: "accepts body without declared length", body: "body"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := artifactcache.ValidateBody(
				strings.NewReader(test.body),
				int64(len(test.body)),
				test.headers,
				nil,
			)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, test.wantErr)
			}
		})
	}
}

func TestXMLRootValidator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "accepts project after declaration and comment",
			body: "\uFEFF<?xml version=\"1.0\"?><!-- pom --><project><modelVersion>4.0.0</modelVersion></project>",
		},
		{
			name:    "rejects html error document",
			body:    "<html><body>upstream error</body></html>",
			wantErr: true,
		},
		{name: "rejects malformed xml", body: "<project>", wantErr: true},
	}

	validator := artifactcache.XMLRootValidator("project")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validator(strings.NewReader(test.body), int64(len(test.body)))
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, test.wantErr)
			}
		})
	}
}

func TestZIPValidator(t *testing.T) {
	t.Parallel()

	const emptyZIP = "PK\x05\x06\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"

	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "accepts empty zip archive", body: emptyZIP},
		{name: "rejects non zip body", body: "upstream error", wantErr: true},
		{name: "rejects truncated zip body", body: "PK\x03\x04", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := artifactcache.ValidateZIP(strings.NewReader(test.body), int64(len(test.body)))
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, test.wantErr)
			}
		})
	}
}
