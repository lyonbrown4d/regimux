package store

import (
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestNewObjectStoreDiskCacheWrapping(t *testing.T) {
	t.Run("disabled preserves local store", func(t *testing.T) {
		store, err := NewObjectStore(ObjectStoreDependencies{
			Config: config.StoreObjectConfig{Driver: "local", Path: t.TempDir()},
		})
		if err != nil {
			t.Fatalf("create local object store: %v", err)
		}
		if _, ok := store.(*object.LocalStore); !ok {
			t.Fatalf("disabled disk cache changed store type: %T", store)
		}
	})

	t.Run("enabled wraps S3 store", func(t *testing.T) {
		store, err := NewObjectStore(ObjectStoreDependencies{
			Config: config.StoreObjectConfig{
				Driver: "s3",
				S3: config.StoreObjectS3Config{
					Bucket: "regimux-objects", Region: "us-east-1",
					AccessKeyID: "test-access-key", SecretAccessKey: "test-secret-key",
				},
				DiskCache: config.StoreObjectDiskCacheConfig{
					Enabled: true, Directory: t.TempDir(), MaxBytes: 2 << 20,
					MaxObjectBytes: 1 << 20, MaxEntries: 10, MaxConcurrentFills: 2, TTL: 24 * time.Hour,
				},
			},
		})
		if err != nil {
			t.Fatalf("create S3 object store with disk cache: %v", err)
		}
		if _, ok := store.(*object.DiskCacheStore); !ok {
			t.Fatalf("enabled disk cache returned store type %T, want *object.DiskCacheStore", store)
		}
		if err := closeObjectStore(t.Context(), store, nil); err != nil {
			t.Fatalf("close disk cache object store: %v", err)
		}
	})
}
