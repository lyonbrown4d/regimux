// Package dockerintegration manages optional host Docker daemon integration.
package dockerintegration

import (
	"context"
	"errors"
	"io"

	dockerevents "github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	dockerimage "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/samber/oops"
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
	opts = append(opts, client.WithAPIVersionNegotiation())

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, daemonStatus{}, oops.In("docker").Wrapf(err, "create docker client")
	}
	ping, err := cli.Ping(ctx)
	if err != nil {
		if closeErr := cli.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
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
	messages, errs := c.client.Events(ctx, dockerevents.ListOptions{
		Filters: filters.NewArgs(filters.Arg("type", string(dockerevents.ImageEventType))),
	})
	go translateDockerEvents(ctx, messages, errs, out, outErrs)
	return out, outErrs
}

func (c *dockerDaemonClient) ImagePull(ctx context.Context, ref, platform string) (io.ReadCloser, error) {
	opts := dockerimage.PullOptions{}
	if platform != "" {
		opts.Platform = platform
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
	for _, key := range []string{"name", "ref", "image"} {
		if value := message.Actor.Attributes[key]; value != "" {
			return value
		}
	}
	return message.Actor.ID
}
