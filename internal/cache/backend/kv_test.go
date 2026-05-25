package backend

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arcgolabs/kvx"
)

func TestKVBackendPrefixAndNil(t *testing.T) {
	ctx := context.Background()
	client := &fakeKVClient{values: map[string][]byte{}}
	cache := NewKV(client, "regimux")

	_, ok, err := cache.Get(ctx, "missing")
	if err != nil {
		t.Fatalf("get missing value: %v", err)
	}
	if ok {
		t.Fatal("expected cache miss")
	}

	if err := cache.Set(ctx, "manifest", []byte("body"), time.Minute); err != nil {
		t.Fatalf("set value: %v", err)
	}
	if _, ok := client.values["regimux:manifest"]; !ok {
		t.Fatal("expected prefixed key in kv client")
	}

	got, ok, err := cache.Get(ctx, "manifest")
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if !ok || !bytes.Equal(got, []byte("body")) {
		t.Fatalf("unexpected hit=%v value=%q", ok, got)
	}
}

func TestKVBackendCopiesValues(t *testing.T) {
	ctx := context.Background()
	client := &fakeKVClient{values: map[string][]byte{}}
	cache := NewKV(client, "")

	value := []byte("abc")
	if err := cache.Set(ctx, "copy", value, time.Minute); err != nil {
		t.Fatalf("set value: %v", err)
	}
	value[0] = 'z'

	got, ok, err := cache.Get(ctx, "copy")
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if !ok || !bytes.Equal(got, []byte("abc")) {
		t.Fatalf("unexpected stored value hit=%v value=%q", ok, got)
	}
}

type fakeKVClient struct {
	values map[string][]byte
}

func (c *fakeKVClient) Get(_ context.Context, key string) ([]byte, error) {
	value, ok := c.values[key]
	if !ok {
		return nil, kvx.ErrNil
	}
	return value, nil
}

func (c *fakeKVClient) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	if key == "error" {
		return errors.New("set error")
	}
	c.values[key] = value
	return nil
}

func (c *fakeKVClient) Delete(_ context.Context, key string) error {
	delete(c.values, key)
	return nil
}

func (c *fakeKVClient) Close() error {
	return nil
}
