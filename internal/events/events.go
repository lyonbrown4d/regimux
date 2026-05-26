// Package events defines the application event bus and events.
package events

import (
	"context"
	"log/slog"

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
	Alias  string
	Method string
	Path   string
	Status int
}

func (UpstreamRequest) Name() string { return "upstream.request" }

func eventName(event Event) string {
	if event == nil {
		return ""
	}
	return event.Name()
}
