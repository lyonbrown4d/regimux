package probehealth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	goredis "github.com/redis/go-redis/v9"
	"github.com/samber/oops"
)

const (
	defaultPrefix = "regimux"
	hotStoreTTL   = 24 * time.Hour
)

// Store keeps endpoint probe health in a low-latency shared layer.
// SQL metadata remains the durable source of truth; this store is best-effort.
type Store interface {
	Put(context.Context, meta.EndpointHealthRecord) error
	List(context.Context, ...string) ([]meta.EndpointHealthRecord, error)
	Close() error
}

type noopStore struct{}

type RedisStore struct {
	client goredis.UniversalClient
	prefix string
	ttl    time.Duration
	logger *slog.Logger
}

type recordList struct {
	values []meta.EndpointHealthRecord
	seen   *collectionset.Set[string]
}

func NewStore(cfg config.Config, logger *slog.Logger) Store {
	logger = componentLogger(logger)
	cache := cfg.Cache
	backend := strings.ToLower(strings.TrimSpace(cache.Backend))
	switch backend {
	case "redis":
		return newRedisStore("redis", cache.Prefix, redisOptions(cache.Redis), logger)
	case "valkey":
		return newRedisStore("valkey", cache.Prefix, redisOptions(cache.Valkey), logger)
	default:
		logger.Debug("probe health hot store disabled", "backend", cache.Backend)
		return noopStore{}
	}
}

func newRedisStore(backend, prefix string, opts *goredis.UniversalOptions, logger *slog.Logger) Store {
	if opts == nil || len(opts.Addrs) == 0 {
		logger.Warn("probe health hot store disabled because remote cache addrs are empty", "backend", backend)
		return noopStore{}
	}
	store := &RedisStore{
		client: goredis.NewUniversalClient(opts),
		prefix: normalizePrefix(prefix),
		ttl:    hotStoreTTL,
		logger: logger,
	}
	logger.Info("probe health hot store enabled", "backend", backend, "addrs", opts.Addrs, "prefix", store.prefix)
	return store
}

func redisOptions(cfg config.ExternalCacheConfig) *goredis.UniversalOptions {
	return &goredis.UniversalOptions{
		Addrs:    cfg.Addrs,
		Username: cfg.Username,
		Password: cfg.Password,
		DB:       cfg.DB,
	}
}

func (noopStore) Put(context.Context, meta.EndpointHealthRecord) error {
	return nil
}

func (noopStore) List(context.Context, ...string) ([]meta.EndpointHealthRecord, error) {
	return nil, nil
}

func (noopStore) Close() error {
	return nil
}

func (s *RedisStore) Put(ctx context.Context, record meta.EndpointHealthRecord) error {
	if s == nil || s.client == nil {
		return nil
	}
	ctx = normalizeContext(ctx)
	key := endpointHealthKey(record)
	if key == "" {
		return nil
	}
	record.Key = key
	record.Alias = strings.TrimSpace(record.Alias)
	record.Registry = normalizeRegistry(record.Registry)
	record.Repository = normalizeRepository(record.Repository)
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now
	}

	hashKey := s.healthKey(key)
	aliasKey := encodedToken(record.Alias)
	indexKey := s.indexKey(aliasKey)
	rankKey := s.rankKey(aliasKey)

	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, hashKey, marshalRecord(record))
	pipe.Expire(ctx, hashKey, s.ttl)
	pipe.SAdd(ctx, indexKey, hashKey)
	pipe.Expire(ctx, indexKey, s.ttl)
	pipe.ZAdd(ctx, rankKey, goredis.Z{
		Score:  endpointHealthScore(record, now),
		Member: record.Registry,
	})
	pipe.Expire(ctx, rankKey, s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return oops.In("probehealth").With("alias", record.Alias).With("registry", record.Registry).Wrapf(err, "write probe health hot state")
	}
	return nil
}

func (s *RedisStore) List(ctx context.Context, aliases ...string) ([]meta.EndpointHealthRecord, error) {
	if s == nil || s.client == nil || len(aliases) == 0 {
		return nil, nil
	}
	ctx = normalizeContext(ctx)
	records := newRecordList()
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		if err := s.appendAliasRecords(ctx, alias, records); err != nil {
			return nil, err
		}
	}
	return records.values, nil
}

func (s *RedisStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	if err := s.client.Close(); err != nil {
		return oops.In("probehealth").Wrapf(err, "close redis probe health store")
	}
	return nil
}

func (s *RedisStore) record(ctx context.Context, key string) (meta.EndpointHealthRecord, bool, error) {
	fields, err := s.client.HGetAll(ctx, key).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return meta.EndpointHealthRecord{}, false, nil
		}
		return meta.EndpointHealthRecord{}, false, oops.In("probehealth").With("key", key).Wrapf(err, "read probe health hot state")
	}
	if len(fields) == 0 {
		return meta.EndpointHealthRecord{}, false, nil
	}
	record, err := unmarshalRecord(fields)
	if err != nil {
		return meta.EndpointHealthRecord{}, false, oops.In("probehealth").With("key", key).Wrapf(err, "parse probe health hot state")
	}
	return record, true, nil
}

func (s *RedisStore) appendAliasRecords(ctx context.Context, alias string, records *recordList) error {
	keys, err := s.client.SMembers(ctx, s.indexKey(encodedToken(alias))).Result()
	if err != nil {
		return oops.In("probehealth").With("alias", alias).Wrapf(err, "list probe health hot state")
	}
	for _, key := range keys {
		if records.seenKey(key) {
			continue
		}
		record, ok, err := s.record(ctx, key)
		if err != nil {
			return err
		}
		if ok {
			records.add(record)
		}
	}
	return nil
}

func newRecordList() *recordList {
	return &recordList{
		values: make([]meta.EndpointHealthRecord, 0),
		seen:   collectionset.NewSet[string](),
	}
}

func (r *recordList) seenKey(key string) bool {
	if r == nil {
		return true
	}
	if r.seen == nil {
		r.seen = collectionset.NewSet[string]()
	}
	if r.seen.Contains(key) {
		return true
	}
	r.seen.Add(key)
	return false
}

func (r *recordList) add(record meta.EndpointHealthRecord) {
	if r == nil {
		return
	}
	r.values = append(r.values, record)
}

func (s *RedisStore) healthKey(recordKey string) string {
	return s.key("probe", "health", "record", encodedToken(recordKey))
}

func (s *RedisStore) indexKey(aliasKey string) string {
	return s.key("probe", "health", "index", aliasKey)
}

func (s *RedisStore) rankKey(aliasKey string) string {
	return s.key("probe", "health", "rank", aliasKey)
}

func (s *RedisStore) key(parts ...string) string {
	key := strings.Join(parts, ":")
	if s == nil || s.prefix == "" {
		return key
	}
	return s.prefix + ":" + key
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func componentLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger.With("component", "probehealth")
}

var _ Store = noopStore{}
var _ Store = (*RedisStore)(nil)
