package upstream

import (
	"context"
	"errors"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/samber/lo"
	"github.com/samber/oops"
	"go.uber.org/multierr"
)

const endpointHealthPersistTimeout = 15 * time.Second

func (c *Client) FlushEndpointHealth(ctx context.Context) error {
	if c == nil || c.metadata == nil {
		return nil
	}
	records := c.drainEndpointHealth()
	if records.Len() == 0 {
		return nil
	}

	persistCtx, cancel := endpointHealthPersistenceContext(ctx)
	defer cancel()
	var flushErr error
	records.Range(func(_ int, record meta.EndpointHealthRecord) bool {
		if err := persistEndpointHealth(persistCtx, c.metadata, record); err != nil {
			c.requeueEndpointHealth(record)
			flushErr = multierr.Append(flushErr, err)
		}
		return persistCtx.Err() == nil
	})
	if flushErr != nil {
		c.logEndpointHealthPersistError(records.Len(), flushErr)
		return oops.In("upstream").Wrapf(flushErr, "flush endpoint health metadata")
	}
	if c.logger != nil {
		c.logger.DebugContext(persistCtx, "flushed upstream endpoint health buffer", "records", records.Len())
	}
	return nil
}

type endpointHealthSnapshotWriter interface {
	PutEndpointHealthSnapshot(context.Context, meta.EndpointHealthRecord) error
}

func persistEndpointHealth(ctx context.Context, metadata meta.Store, record meta.EndpointHealthRecord) error {
	if writer, ok := metadata.(endpointHealthSnapshotWriter); ok {
		if err := writer.PutEndpointHealthSnapshot(ctx, record); err != nil {
			return oops.In("upstream").Wrapf(err, "put endpoint health snapshot")
		}
		return nil
	}
	_, err := metadata.UpsertEndpointHealth(ctx, record)
	if err != nil {
		return oops.In("upstream").Wrapf(err, "upsert endpoint health")
	}
	return nil
}

func (c *Client) enqueueEndpointHealth(record meta.EndpointHealthRecord) {
	key := endpointHealthRecordKey(record)
	if key == "" {
		return
	}
	record.Key = key
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	if c.healthPending == nil {
		c.healthPending = collectionmapping.NewConcurrentMap[string, meta.EndpointHealthRecord]()
	}
	c.healthPending.Set(key, record)
}

func (c *Client) requeueEndpointHealth(record meta.EndpointHealthRecord) {
	key := endpointHealthRecordKey(record)
	if key == "" {
		return
	}
	record.Key = key
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	if c.healthPending == nil {
		c.healthPending = collectionmapping.NewConcurrentMap[string, meta.EndpointHealthRecord]()
	}
	if _, exists := c.healthPending.Get(key); !exists {
		c.healthPending.Set(key, record)
	}
}

func (c *Client) drainEndpointHealth() *collectionlist.List[meta.EndpointHealthRecord] {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	if c.healthPending == nil || c.healthPending.Len() == 0 {
		return collectionlist.NewList[meta.EndpointHealthRecord]()
	}
	records := lo.MapToSlice(c.healthPending.All(), func(key string, record meta.EndpointHealthRecord) meta.EndpointHealthRecord {
		record.Key = key
		return record
	})
	c.healthPending.Clear()
	return collectionlist.NewList(records...)
}

func endpointHealthPersistenceContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(parent), endpointHealthPersistTimeout)
}

func endpointHealthRecordKey(record meta.EndpointHealthRecord) string {
	alias := strings.TrimSpace(record.Alias)
	registry := normalizeEndpointHealthRegistry(record.Registry)
	if alias == "" || registry == "" {
		return ""
	}
	return meta.EndpointHealthKey{
		Alias:      alias,
		Registry:   registry,
		Repository: strings.Trim(strings.TrimSpace(record.Repository), "/"),
	}.String()
}

func (c *Client) logEndpointHealthPersistError(records int, err error) {
	if c == nil || c.logger == nil {
		return
	}
	args := []any{
		"records", records,
		"error", err,
	}
	if errors.Is(err, context.Canceled) {
		c.logger.Debug("flush upstream endpoint health buffer skipped after context cancellation", args...)
		return
	}
	c.logger.Warn("flush upstream endpoint health buffer failed", args...)
}
