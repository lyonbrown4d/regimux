package ecosystem

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/probehealth"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/panjf2000/ants/v2"
	"github.com/samber/oops"
	"go.uber.org/multierr"
)

const endpointHealthAlpha = 0.2

type EndpointProber struct {
	metadata  meta.EndpointHealthRepository
	hotHealth probehealth.Store
	workers   *worker.Pools
	client    *http.Client
	logger    *slog.Logger
}

func NewEndpointProber(metadata meta.EndpointHealthRepository, pools *worker.Pools, logger *slog.Logger, stores ...probehealth.Store) *EndpointProber {
	if logger == nil {
		logger = slog.Default()
	}
	var hotHealth probehealth.Store
	if len(stores) > 0 {
		hotHealth = stores[0]
	}
	return &EndpointProber{
		metadata:  metadata,
		hotHealth: hotHealth,
		workers:   pools,
		client:    &http.Client{},
		logger:    logger.With("component", "ecosystem-probe"),
	}
}

func (p *EndpointProber) Probe(ctx context.Context, target ProbeTarget) error {
	if p == nil {
		return oops.In("ecosystem").Errorf("endpoint prober is not configured")
	}
	endpoints := probeEndpoints(target.Config)
	if endpoints.Len() == 0 {
		err := oops.In("ecosystem").With("ecosystem", target.Ecosystem, "alias", target.Alias).Errorf("probe endpoints are not configured")
		p.logProbeStarted(ctx, target, endpoints.Len(), err)
		return err
	}
	p.logProbeStarted(ctx, target, endpoints.Len(), nil)

	var successes atomic.Int32
	var failures atomic.Int32
	tasks := collectionlist.NewListWithCapacity[func(context.Context) error](endpoints.Len())
	endpoints.Range(func(_ int, endpoint string) bool {
		probeEndpoint := endpoint
		tasks.Add(func(taskCtx context.Context) error {
			if err := p.probeEndpoint(taskCtx, target, probeEndpoint); err != nil {
				failures.Add(1)
				return err
			}
			successes.Add(1)
			return nil
		})
		return true
	})

	probeErr := worker.RunAllSettled(ctx, p.probePool(), tasks)
	successCount := int(successes.Load())
	failureCount := int(failures.Load())
	if successCount > 0 {
		p.logSummary(ctx, target, successCount, failureCount, nil)
		return nil
	}
	probeErr = multierr.Combine(oops.In("ecosystem").With("ecosystem", target.Ecosystem, "alias", target.Alias).Errorf("probe ecosystem endpoints"), probeErr)
	p.logSummary(ctx, target, successCount, failureCount, probeErr)
	return oops.Wrapf(probeErr, "probe ecosystem upstream")
}

func (p *EndpointProber) probeEndpoint(ctx context.Context, target ProbeTarget, endpoint string) error {
	probeCtx, cancel := p.probeContext(ctx, target.Config)
	defer cancel()

	startedAt := time.Now()
	probeURLValue := probeURL(endpoint)
	if p.logger != nil {
		p.logger.DebugContext(ctx,
			"ecosystem probe request started",
			"ecosystem", target.Ecosystem,
			"alias", target.Alias,
			"registry", endpoint,
			"url", probeURLValue,
			"timeout", target.Config.Probe.Timeout,
		)
	}
	req, err := http.NewRequestWithContext(probeCtx, http.MethodHead, probeURLValue, http.NoBody)
	if err != nil {
		p.recordFailure(ctx, target, endpoint, time.Now())
		p.logEndpoint(ctx, target, endpoint, 0, 0, err)
		return oops.Wrapf(err, "create ecosystem probe request")
	}
	req.Header.Set("User-Agent", "regimux/dev")
	applyProbeAuth(req, target.Config.Auth)

	resp, err := p.clientFor(target.Config).Do(req)
	latency := time.Since(startedAt)
	now := time.Now()
	if err != nil {
		p.recordFailure(ctx, target, endpoint, now)
		p.logEndpoint(ctx, target, endpoint, latency, 0, err)
		return oops.Wrapf(err, "send ecosystem probe request")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && p.logger != nil {
			p.logger.Debug("close ecosystem probe response body failed", "error", closeErr)
		}
	}()
	if probeStatusReachable(resp.StatusCode) {
		if recordErr := p.recordSuccess(ctx, target, endpoint, latency, now); recordErr != nil {
			return recordErr
		}
		p.logEndpoint(ctx, target, endpoint, latency, resp.StatusCode, nil)
		return nil
	}

	err = oops.With("status", resp.StatusCode).Errorf("ecosystem probe endpoint returned unreachable status")
	p.recordFailure(ctx, target, endpoint, now)
	p.logEndpoint(ctx, target, endpoint, latency, resp.StatusCode, err)
	return err
}

func (p *EndpointProber) probeContext(ctx context.Context, cfg config.UpstreamConfig) (context.Context, context.CancelFunc) {
	timeout := cfg.Probe.Timeout
	if timeout <= 0 {
		timeout = cfg.HTTP.Timeout
	}
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (p *EndpointProber) clientFor(cfg config.UpstreamConfig) *http.Client {
	if cfg.HTTP.Timeout <= 0 {
		return p.client
	}
	return &http.Client{Timeout: cfg.HTTP.Timeout}
}

func (p *EndpointProber) probePool() *ants.Pool {
	if p == nil || p.workers == nil {
		return nil
	}
	return p.workers.ProbePool()
}

func (p *EndpointProber) recordSuccess(ctx context.Context, target ProbeTarget, endpoint string, latency time.Duration, at time.Time) error {
	if p == nil {
		return nil
	}
	record, err := p.endpointRecord(ctx, target, endpoint)
	if err != nil {
		return err
	}
	record.LatencyEWMA = nextLatencyEWMA(record.LatencyEWMA, record.LatencySamples, latency)
	record.LatencySamples++
	record.ConsecutiveFailures = 0
	record.SuccessCount++
	record.CooldownUntil = time.Time{}
	record.DegradedUntil = time.Time{}
	record.LastSuccessAt = at.UTC()
	record.LastProbeAt = at.UTC()
	persistErr := p.persistRecord(ctx, target, endpoint, record, "persist ecosystem endpoint probe success")
	p.logHealthSnapshot(ctx, target, at, "ecosystem endpoint probe success", record, persistErr)
	return persistErr
}

func (p *EndpointProber) recordFailure(ctx context.Context, target ProbeTarget, endpoint string, at time.Time) {
	if p == nil {
		return
	}
	record, err := p.endpointRecord(ctx, target, endpoint)
	if err != nil {
		p.logPersistError(ctx, target, endpoint, err)
		return
	}
	record.ConsecutiveFailures++
	record.FailureCount++
	record.LastFailureAt = at.UTC()
	record.LastProbeAt = at.UTC()
	if target.Config.Probe.Cooldown > 0 {
		record.CooldownUntil = at.Add(target.Config.Probe.Cooldown).UTC()
	}
	persistErr := p.persistRecord(ctx, target, endpoint, record, "persist ecosystem endpoint probe failure")
	if persistErr != nil {
		p.logPersistError(ctx, target, endpoint, persistErr)
	}
	p.logHealthSnapshot(ctx, target, at, "ecosystem endpoint probe failure", record, persistErr)
}

func (p *EndpointProber) persistRecord(ctx context.Context, target ProbeTarget, endpoint string, record meta.EndpointHealthRecord, message string) error {
	if p == nil {
		return nil
	}
	if p.metadata != nil {
		persisted, err := p.metadata.UpsertEndpointHealth(ctx, record)
		if err != nil {
			return oops.In("ecosystem").With("op", message).Wrapf(err, "persist ecosystem endpoint probe")
		}
		if persisted != nil {
			record = *persisted
		}
	}
	if p.hotHealth != nil {
		if err := p.hotHealth.Put(ctx, record); err != nil && p.logger != nil {
			p.logger.DebugContext(ctx, "write ecosystem endpoint probe hot state failed", "ecosystem", target.Ecosystem, "alias", target.Alias, "registry", endpoint, "error", err)
		}
	}
	return nil
}

func (p *EndpointProber) endpointRecord(ctx context.Context, target ProbeTarget, endpoint string) (meta.EndpointHealthRecord, error) {
	key := meta.EndpointHealthKey{
		Alias:    ScopedAlias(target.Ecosystem, target.Alias),
		Registry: strings.TrimRight(strings.TrimSpace(endpoint), "/"),
	}
	if p == nil || p.metadata == nil {
		return meta.EndpointHealthRecord{Alias: key.Alias, Registry: key.Registry}, nil
	}
	record, ok, err := p.metadata.EndpointHealth(ctx, key)
	if err != nil {
		return meta.EndpointHealthRecord{}, oops.Wrapf(err, "load ecosystem endpoint health")
	}
	if ok && record != nil {
		return *record, nil
	}
	return meta.EndpointHealthRecord{Alias: key.Alias, Registry: key.Registry}, nil
}
