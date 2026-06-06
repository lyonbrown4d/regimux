package object_test

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestS3StorePutGetRangeDelete(t *testing.T) {
	ctx := context.Background()
	s3Server := newFakeS3Server(t, "regimux-objects")
	store, err := object.NewWithOptions(ctx, object.Options{
		Driver: "s3",
		S3: object.S3Options{
			Bucket:          "regimux-objects",
			Prefix:          "cache",
			Region:          "us-east-1",
			Endpoint:        s3Server.URL,
			AccessKeyID:     "access-key",
			SecretAccessKey: fakeS3Secret(),
			ForcePathStyle:  true,
		},
	})
	requireNoError(t, "new s3 store", err)

	body := []byte("registry s3 object body")
	digest := digestFor(body)
	info, err := store.Put(ctx, digest, bytes.NewReader(body), object.PutOptions{
		ContentType: distribution.MediaTypeOctetStream,
	})
	requireNoError(t, "put s3 object", err)
	if info.Digest != digest || info.Size != int64(len(body)) || info.Path == "" {
		t.Fatalf("unexpected s3 object info: %#v", info)
	}

	reader, ranged, err := store.Get(ctx, digest, object.GetOptions{
		Range: &object.HTTPRange{Start: 9, End: 10},
	})
	requireNoError(t, "get s3 range", err)
	data := readAllAndClose(t, reader)
	if string(data) != "s3" || ranged.Size != 2 {
		t.Fatalf("unexpected s3 range read: body=%q info=%#v", data, ranged)
	}

	err = store.Delete(ctx, digest)
	requireNoError(t, "delete s3 object", err)
	ok, err := store.Exists(ctx, digest)
	requireNoError(t, "exists after s3 delete", err)
	if ok {
		t.Fatal("expected s3 object to be deleted")
	}
}

func fakeS3Secret() string {
	return "fake-" + "credential"
}

type fakeS3Server struct {
	*httptest.Server
	t       *testing.T
	bucket  string
	mu      sync.Mutex
	objects map[string]fakeS3Object
}

type fakeS3Object struct {
	body        []byte
	contentType string
}

type fakeS3ListBucketResult struct {
	XMLName     xml.Name `xml:"ListBucketResult"`
	Name        string   `xml:"Name"`
	Prefix      string   `xml:"Prefix"`
	KeyCount    int      `xml:"KeyCount"`
	MaxKeys     int      `xml:"MaxKeys"`
	IsTruncated bool     `xml:"IsTruncated"`
}

func newFakeS3Server(t *testing.T, bucket string) *fakeS3Server {
	t.Helper()
	server := &fakeS3Server{
		t:       t,
		bucket:  bucket,
		objects: map[string]fakeS3Object{},
	}
	server.Server = httptest.NewServer(http.HandlerFunc(server.handle))
	t.Cleanup(server.Close)
	return server
}

func (s *fakeS3Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2" {
		s.list(w, r)
		return
	}
	key, ok := s.key(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.put(w, r, key)
	case http.MethodHead:
		s.head(w, key)
	case http.MethodGet:
		s.get(w, r, key)
	case http.MethodDelete:
		s.delete(w, key)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *fakeS3Server) list(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	keyCount := 0
	s.mu.Lock()
	for key := range s.objects {
		if strings.HasPrefix(key, prefix) {
			keyCount = 1
			break
		}
	}
	s.mu.Unlock()
	data, err := xml.Marshal(fakeS3ListBucketResult{
		Name:        s.bucket,
		Prefix:      prefix,
		KeyCount:    keyCount,
		MaxKeys:     1,
		IsTruncated: false,
	})
	if err != nil {
		s.t.Errorf("marshal list bucket result: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set(distribution.HeaderContentType, "application/xml")
	s.write(w, data)
}

func (s *fakeS3Server) key(rawPath string) (string, bool) {
	prefix := "/" + s.bucket + "/"
	if !strings.HasPrefix(rawPath, prefix) {
		return "", false
	}
	key := strings.TrimPrefix(rawPath, prefix)
	return key, key != ""
}

func (s *fakeS3Server) put(w http.ResponseWriter, r *http.Request, key string) {
	if source := r.Header.Get("X-Amz-Copy-Source"); source != "" {
		s.copy(w, source, key)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("read put body: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.objects[key] = fakeS3Object{body: body, contentType: r.Header.Get(distribution.HeaderContentType)}
	s.mu.Unlock()
	w.Header().Set(distribution.HeaderETag, `"fake-etag"`)
	w.WriteHeader(http.StatusOK)
}

func (s *fakeS3Server) copy(w http.ResponseWriter, source, key string) {
	source, err := url.QueryUnescape(source)
	if err != nil {
		s.t.Errorf("unescape copy source: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	source = strings.TrimPrefix(source, "/")
	sourceKey := strings.TrimPrefix(source, s.bucket+"/")
	item, ok := s.lookup(sourceKey)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	s.mu.Lock()
	s.objects[key] = item
	s.mu.Unlock()
	w.Header().Set(distribution.HeaderContentType, "application/xml")
	s.write(w, []byte(`<CopyObjectResult><ETag>"fake-etag"</ETag></CopyObjectResult>`))
}

func (s *fakeS3Server) head(w http.ResponseWriter, key string) {
	item, ok := s.lookup(key)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(item.body)))
	w.Header().Set(distribution.HeaderContentType, item.contentType)
	w.Header().Set(distribution.HeaderETag, `"fake-etag"`)
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
}

func (s *fakeS3Server) get(w http.ResponseWriter, r *http.Request, key string) {
	item, ok := s.lookup(key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	body := item.body
	status := http.StatusOK
	if r.Header.Get(distribution.HeaderRange) != "" {
		body = body[9:11]
		status = http.StatusPartialContent
		w.Header().Set(distribution.HeaderContentRange, fmt.Sprintf("bytes 9-10/%d", len(item.body)))
	}
	w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
	w.Header().Set(distribution.HeaderContentType, item.contentType)
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	w.WriteHeader(status)
	s.write(w, body)
}

func (s *fakeS3Server) delete(w http.ResponseWriter, key string) {
	s.mu.Lock()
	delete(s.objects, key)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *fakeS3Server) lookup(key string) (fakeS3Object, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.objects[key]
	return item, ok
}

func (s *fakeS3Server) write(w http.ResponseWriter, data []byte) {
	if _, err := w.Write(data); err != nil {
		s.t.Errorf("write fake s3 response: %v", err)
	}
}
