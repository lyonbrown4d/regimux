// Package events defines the application event bus and events.
package events

import (
	"context"
	"log/slog"
	"time"

	"github.com/arcgolabs/eventx"
	"github.com/samber/oops"
)

type Event = eventx.Event
type Bus = eventx.BusRuntime

func NewBus(logger *slog.Logger) Bus {
	if logger == nil {
		logger = slog.Default()
	}
	return eventx.New(
		eventx.WithAntsPool(4),
		eventx.WithMiddleware(
			eventx.RecoverMiddleware(),
			eventx.ObserveMiddleware(func(ctx context.Context, event Event, duration time.Duration, err error) {
				if err != nil {
					logger.ErrorContext(ctx, "event dispatch failed", "event", eventName(event), "duration", duration, "error", err)
					return
				}
				logger.DebugContext(ctx, "event dispatched", "event", eventName(event), "duration", duration)
			}),
		),
		eventx.WithAsyncErrorHandler(func(ctx context.Context, event Event, err error) {
			logger.ErrorContext(ctx, "async event dispatch failed", "event", eventName(event), "error", err)
		}),
	)
}

func Publish(ctx context.Context, bus Bus, event Event) error {
	if bus == nil || event == nil {
		return nil
	}
	if err := bus.Publish(ctx, event); err != nil {
		return oops.Wrapf(err, "publish event %s", eventName(event))
	}
	return nil
}

type ApplicationStarting struct {
	Version string
}

func (ApplicationStarting) Name() string {
	return "regimux.application.starting"
}

type ApplicationStarted struct {
	Version string
}

func (ApplicationStarted) Name() string {
	return "regimux.application.started"
}

type ApplicationStopping struct {
	Version string
}

func (ApplicationStopping) Name() string {
	return "regimux.application.stopping"
}

type ApplicationStopped struct {
	Version string
}

func (ApplicationStopped) Name() string {
	return "regimux.application.stopped"
}

type ServerStarted struct {
	Listen string
}

func (ServerStarted) Name() string { return "server.started" }

type ServerStopped struct {
	Listen string
}

func (ServerStopped) Name() string { return "server.stopped" }

type UpstreamRequest struct {
	Alias     string
	Operation string
	Registry  string
	Method    string
	Path      string
	Status    int
	Attempts  int
	Duration  time.Duration
	Size      int64
	Error     string
}

func (UpstreamRequest) Name() string { return "upstream.request" }

type UpstreamFailover struct {
	Alias     string
	Operation string
	Registry  string
	Error     string
	HasNext   bool
}

func (UpstreamFailover) Name() string { return "upstream.failover" }

type CacheAccess struct {
	Kind       string
	Alias      string
	Repository string
	Reference  string
	Digest     string
	Status     string
}

func (CacheAccess) Name() string { return "cache.access" }

type CacheStore struct {
	Kind       string
	Alias      string
	Repository string
	Reference  string
	Digest     string
	Size       int64
}

func (CacheStore) Name() string { return "cache.store" }

type ArtifactPulled struct {
	Ecosystem  string
	Kind       string
	Alias      string
	Repository string
	Reference  string
	Status     string
	Accept     string
}

func (ArtifactPulled) Name() string { return "artifact.pulled" }

type DependencyPulled struct {
	Ecosystem  string
	Kind       string
	Alias      string
	Repository string
	Reference  string
	Status     string
}

func (DependencyPulled) Name() string { return "dependency.pulled" }

type DependencyPullDenied struct {
	Ecosystem  string
	Kind       string
	Alias      string
	Repository string
	Reference  string
	Reason     string
}

func (DependencyPullDenied) Name() string { return "dependency.pull_denied" }

type DockerImageEvent struct {
	Action string
	Actor  string
	Ref    string
}

func (DockerImageEvent) Name() string { return "docker.image_event" }

type DockerPrewarm struct {
	Alias    string
	Image    string
	ProxyRef string
	Status   string
	Duration time.Duration
	Error    string
}

func (DockerPrewarm) Name() string { return "docker.prewarm" }

func eventName(event Event) string {
	if event == nil {
		return ""
	}
	return event.Name()
}
