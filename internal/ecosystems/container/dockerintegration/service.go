package dockerintegration

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/samber/oops"
)

type Service struct {
	cfg              config.Config
	logger           *slog.Logger
	bus              events.Bus
	metrics          *observability.Metrics
	connector        connector
	mu               sync.Mutex
	client           daemonClient
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	defaultNamespace string
}

type ImageEvent struct {
	Action string
	Actor  string
	Ref    string
}

type PrewarmRequest struct {
	Image string
}

type PrewarmResult struct {
	ProxyRef string
	Duration time.Duration
}

func NewService(
	cfg config.Config,
	logger *slog.Logger,
	bus events.Bus,
	metrics *observability.Metrics,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	upstreamCfg, _ := cfg.ContainerUpstream(cfg.Docker.Prewarm.Alias)
	return &Service{
		cfg:              cfg,
		logger:           logger,
		bus:              bus,
		metrics:          metrics,
		connector:        dockerConnector{},
		defaultNamespace: upstreamCfg.DefaultNamespace,
	}
}

func (s *Service) Start(ctx context.Context) error {
	if s == nil || !s.cfg.Docker.Enabled {
		return nil
	}
	client, status, err := s.connector.Connect(ctx, s.cfg.Docker)
	if err != nil {
		s.metrics.ObserveDockerDaemon(ctx, s.cfg.Docker.Host, false)
		return oops.In("docker").Wrapf(err, "connect docker daemon")
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.mu.Lock()
	s.client = client
	s.cancel = cancel
	s.mu.Unlock()

	s.metrics.ObserveDockerDaemon(ctx, s.cfg.Docker.Host, true)
	s.logger.InfoContext(ctx,
		"docker daemon integration connected",
		"host", dockerHostLabel(s.cfg.Docker.Host),
		"api_version", status.APIVersion,
		"os", status.OSType,
		"observe", s.cfg.Docker.Observe,
		"prewarm", s.cfg.Docker.Prewarm.Enabled,
	)
	s.startBackgroundWork(runCtx, client)
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	cancel := s.cancel
	client := s.client
	s.client = nil
	s.cancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if err := s.wait(ctx); err != nil {
		return err
	}
	if client != nil {
		if err := client.Close(); err != nil {
			return oops.In("docker").Wrapf(err, "close docker daemon client")
		}
	}
	s.metrics.ObserveDockerDaemon(ctx, s.cfg.Docker.Host, false)
	return nil
}

func (s *Service) Prewarm(ctx context.Context, req PrewarmRequest) (PrewarmResult, error) {
	if s == nil || !s.cfg.Docker.Enabled || !s.cfg.Docker.Prewarm.Enabled {
		return PrewarmResult{}, oops.In("docker").Errorf("docker prewarm is disabled")
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return PrewarmResult{}, oops.In("docker").Errorf("docker daemon is not connected")
	}
	return s.prewarm(ctx, client, req)
}

func (s *Service) startBackgroundWork(ctx context.Context, client daemonClient) {
	if s.cfg.Docker.Observe {
		s.wg.Add(1)
		go s.observe(ctx, client)
	}
	if s.cfg.Docker.Prewarm.Enabled && len(s.cfg.Docker.Prewarm.Images) > 0 {
		s.wg.Add(1)
		go s.prewarmConfiguredImages(ctx, client)
	}
}

func (s *Service) observe(ctx context.Context, client daemonClient) {
	defer s.wg.Done()
	imageEvents, errs := client.ImageEvents(ctx)
	for {
		event, ok, eventErr := nextImageEvent(ctx, imageEvents, errs)
		if !ok {
			return
		}
		if eventErr != nil {
			s.logImageEventStreamError(ctx, eventErr)
			return
		}
		s.handleImageEvent(ctx, event)
	}
}

func (s *Service) logImageEventStreamError(ctx context.Context, err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		s.logger.WarnContext(ctx, "docker image event stream stopped", "error", err)
	}
}

func (s *Service) handleImageEvent(ctx context.Context, event ImageEvent) {
	s.metrics.ObserveDockerImageEvent(ctx, event.Action)
	s.logger.DebugContext(ctx, "docker image event observed", "action", event.Action, "ref", event.Ref, "actor", event.Actor)
	if err := events.Publish(ctx, s.bus, events.DockerImageEvent{
		Action: event.Action,
		Actor:  event.Actor,
		Ref:    event.Ref,
	}); err != nil {
		s.logger.DebugContext(ctx, "publish docker image event failed", "error", err)
	}
}

func (s *Service) prewarmConfiguredImages(ctx context.Context, client daemonClient) {
	defer s.wg.Done()
	for _, image := range s.cfg.Docker.Prewarm.Images {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if _, err := s.prewarm(ctx, client, PrewarmRequest{Image: image}); err != nil {
			s.logger.WarnContext(ctx, "configured docker prewarm failed", "image", image, "error", err)
		}
	}
}

func (s *Service) prewarm(ctx context.Context, client daemonClient, req PrewarmRequest) (PrewarmResult, error) {
	startedAt := time.Now()
	proxyRef, err := s.proxyReference(req.Image)
	if err != nil {
		return PrewarmResult{}, err
	}
	s.logger.InfoContext(ctx, "docker prewarm starting", "image", req.Image, "proxy_ref", proxyRef)
	err = s.pullProxyReference(ctx, client, proxyRef)
	duration := time.Since(startedAt)
	s.observePrewarm(ctx, req.Image, proxyRef, duration, err)
	if err != nil {
		return PrewarmResult{}, err
	}
	s.logger.InfoContext(ctx, "docker prewarm completed", "image", req.Image, "proxy_ref", proxyRef, "duration", duration)
	return PrewarmResult{ProxyRef: proxyRef, Duration: duration}, nil
}

func (s *Service) pullProxyReference(ctx context.Context, client daemonClient, proxyRef string) error {
	pullCtx := ctx
	cancel := func() {}
	if s.cfg.Docker.Prewarm.Timeout > 0 {
		pullCtx, cancel = context.WithTimeout(ctx, s.cfg.Docker.Prewarm.Timeout)
	}
	defer cancel()

	body, err := client.ImagePull(pullCtx, proxyRef, s.cfg.Docker.Prewarm.Platform)
	if err != nil {
		return oops.In("docker").With("reference", proxyRef).Wrapf(err, "pull proxy reference")
	}
	return drainAndClosePullStream(body)
}

func (s *Service) proxyReference(image string) (string, error) {
	proxyRef, err := BuildProxyReference(ProxyReferenceOptions{
		Registry:         s.cfg.Docker.Prewarm.Registry,
		Alias:            s.cfg.Docker.Prewarm.Alias,
		DefaultNamespace: s.defaultNamespace,
	}, image)
	if err != nil {
		return "", oops.In("docker").With("image", image).Wrapf(err, "build docker proxy reference")
	}
	return proxyRef, nil
}

func (s *Service) observePrewarm(ctx context.Context, image, proxyRef string, duration time.Duration, err error) {
	status := "success"
	errorMessage := ""
	if err != nil {
		status = "error"
		errorMessage = err.Error()
	}
	s.metrics.ObserveDockerPrewarm(ctx, s.cfg.Docker.Prewarm.Alias, duration, err)
	if publishErr := events.Publish(ctx, s.bus, events.DockerPrewarm{
		Alias:    s.cfg.Docker.Prewarm.Alias,
		Image:    image,
		ProxyRef: proxyRef,
		Status:   status,
		Duration: duration,
		Error:    errorMessage,
	}); publishErr != nil {
		s.logger.DebugContext(ctx, "publish docker prewarm event failed", "error", publishErr)
	}
}

func (s *Service) wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return oops.In("docker").Wrapf(ctx.Err(), "stop docker integration")
	}
}
