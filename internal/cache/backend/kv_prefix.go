package backend

import (
	"context"

	redisadapter "github.com/arcgolabs/kvx/adapter/redis"
	goredis "github.com/redis/go-redis/v9"
	"github.com/valkey-io/valkey-go"
)

type redisKVClient struct {
	*redisadapter.Adapter
	client *goredis.Client
}

func (c *redisKVClient) DeletePrefix(ctx context.Context, prefix string) error {
	return deletePrefixByScan(ctx, prefix,
		func(ctx context.Context, cursor uint64, pattern string, count int64) ([]string, uint64, error) {
			return c.client.Scan(ctx, cursor, pattern, count).Result()
		},
		func(ctx context.Context, keys []string) error {
			return c.client.Unlink(ctx, keys...).Err()
		},
	)
}

func (c *valkeyKVClient) DeletePrefix(ctx context.Context, prefix string) error {
	return deletePrefixAcrossNodes(ctx, prefix, valkeyNodeScanners(c.client), func(ctx context.Context, keys []string) error {
		return deleteValkeyKeys(ctx, c.client, keys)
	})
}

func valkeyNodeScanners(client valkey.Client) []keyScanner {
	nodes := client.Nodes()
	scanners := make([]keyScanner, 0, len(nodes))
	for _, node := range nodes {
		nodeClient := node
		scanners = append(scanners, valkeyNodeScanner(nodeClient))
	}
	return scanners
}

func valkeyNodeScanner(node valkey.Client) keyScanner {
	return func(ctx context.Context, cursor uint64, pattern string, count int64) ([]string, uint64, error) {
		entry, err := node.Do(ctx, node.B().Scan().Cursor(cursor).Match(pattern).Count(count).Build()).AsScanEntry()
		if err != nil {
			return nil, 0, wrapError(err, "scan valkey cache node")
		}
		return entry.Elements, entry.Cursor, nil
	}
}

func deleteValkeyKeys(ctx context.Context, client valkey.Client, keys []string) error {
	commands := make([]valkey.Completed, 0, len(keys))
	for _, key := range keys {
		commands = append(commands, client.B().Unlink().Key(key).Build())
	}
	for _, result := range client.DoMulti(ctx, commands...) {
		if err := result.Error(); err != nil {
			return wrapError(err, "delete valkey cache keys")
		}
	}
	return nil
}

var (
	_ KVClient       = (*redisKVClient)(nil)
	_ prefixKVClient = (*redisKVClient)(nil)
	_ lockClient     = (*redisKVClient)(nil)
	_ prefixKVClient = (*valkeyKVClient)(nil)
)
