// Package dockerintegration manages optional host Docker daemon integration.
package dockerintegration

import (
	"context"
	"io"

	"github.com/containerd/platforms"
	"github.com/lyonbrown4d/regimux/internal/config"
	dockerevents "github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	"github.com/samber/lo"
	"github.com/samber/oops"
	"go.uber.org/multierr"
)

type daemonClient interface {
	Close() error
	ImageEvents(ctx context.Context) (<-chan ImageEvent, <-chan error)
	ImagePull(ctx context.Context, ref, platform string) (io.ReadCloser, error)
}

type connector interface {
	Connect(ctx context.Context, cfg config.DockerConfig) (daemonClient, daemonStatus, error)
}

type daemonStatus struct {
	APIVersion string
	OSType     string
}

type dockerConnector struct{}

type dockerDaemonClient struct {
	client *client.Client
}

func (dockerConnector) Connect(ctx context.Context, cfg config.DockerConfig) (daemonClient, daemonStatus, error) {
	opts := []client.Opt{client.FromEnv}
	if cfg.Host != "" {
		opts = append(opts, client.WithHost(cfg.Host))
	}
	cli, err := client.New(opts...)
	if err != nil {
		return nil, daemonStatus{}, oops.In("docker").Wrapf(err, "create docker client")
	}
	ping, err := cli.Ping(ctx, client.PingOptions{})
	if err != nil {
		if closeErr := cli.Close(); closeErr != nil {
			err = multierr.Combine(err, closeErr)
		}
		return nil, daemonStatus{}, oops.In("docker").Wrapf(err, "ping docker daemon")
	}
	return &dockerDaemonClient{client: cli}, daemonStatus{
		APIVersion: ping.APIVersion,
		OSType:     ping.OSType,
	}, nil
}

func (c *dockerDaemonClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	if err := c.client.Close(); err != nil {
		return oops.In("docker").Wrapf(err, "close docker client")
	}
	return nil
}

func (c *dockerDaemonClient) ImageEvents(ctx context.Context) (<-chan ImageEvent, <-chan error) {
	out := make(chan ImageEvent)
	outErrs := make(chan error, 1)
	result := c.client.Events(ctx, client.EventsListOptions{
		Filters: make(client.Filters).Add("type", string(dockerevents.ImageEventType)),
	})
	go translateDockerEvents(ctx, result.Messages, result.Err, out, outErrs)
	return out, outErrs
}

func (c *dockerDaemonClient) ImagePull(ctx context.Context, ref, platform string) (io.ReadCloser, error) {
	opts := client.ImagePullOptions{}
	if platform != "" {
		parsed, err := platforms.Parse(platform)
		if err != nil {
			return nil, oops.In("docker").With("platform", platform).Wrapf(err, "parse docker pull platform")
		}
		opts.Platforms = append(opts.Platforms, parsed)
	}
	body, err := c.client.ImagePull(ctx, ref, opts)
	if err != nil {
		return nil, oops.In("docker").With("reference", ref).Wrapf(err, "pull docker image")
	}
	return body, nil
}

func translateDockerEvents(
	ctx context.Context,
	messages <-chan dockerevents.Message,
	errs <-chan error,
	out chan<- ImageEvent,
	outErrs chan<- error,
) {
	defer close(out)
	defer close(outErrs)
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-errs:
			if ok && err != nil {
				outErrs <- oops.In("docker").Wrapf(err, "read docker event stream")
			}
			return
		case message, ok := <-messages:
			if !ok {
				return
			}
			out <- imageEventFromDocker(message)
		}
	}
}

func imageEventFromDocker(message dockerevents.Message) ImageEvent {
	return ImageEvent{
		Action: string(message.Action),
		Actor:  message.Actor.ID,
		Ref:    dockerEventRef(message),
	}
}

func dockerEventRef(message dockerevents.Message) string {
	return lo.CoalesceOrEmpty(
		message.Actor.Attributes["name"],
		message.Actor.Attributes["ref"],
		message.Actor.Attributes["image"],
		message.Actor.ID,
	)
}
