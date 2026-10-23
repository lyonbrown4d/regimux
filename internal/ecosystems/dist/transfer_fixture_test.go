package dist_test

import (
	"io"
	"net/http"
	"strconv"
	"testing"
)

func writeInterruptedDistResponse(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("ETag", `"stable"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(testZIPBody)))
	if _, err := io.WriteString(w, testZIPBody[:5]); err != nil {
		t.Error(err)
		return
	}
	controller := http.NewResponseController(w)
	if err := controller.Flush(); err != nil {
		t.Error(err)
		return
	}
	connection, _, err := controller.Hijack()
	if err != nil {
		t.Error(err)
		return
	}
	if err := connection.Close(); err != nil {
		t.Error(err)
	}
}

func writeResumedDistResponse(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Range"); got != "bytes=5-21" {
		t.Errorf("recovery Range=%q", got)
	}
	if got := r.Header.Get("If-Range"); got != `"stable"` {
		t.Errorf("recovery If-Range=%q", got)
	}
	w.Header().Set("ETag", `"stable"`)
	w.Header().Set("Content-Range", "bytes 5-21/22")
	w.Header().Set("Content-Length", "17")
	w.WriteHeader(http.StatusPartialContent)
	if _, err := io.WriteString(w, testZIPBody[5:]); err != nil {
		t.Error(err)
	}
}
