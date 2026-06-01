package dockerintegration

import (
	"context"
	"errors"
	"io"

	"github.com/samber/oops"
)

func nextImageEvent(ctx context.Context, imageEvents <-chan ImageEvent, errs <-chan error) (ImageEvent, bool, error) {
	select {
	case <-ctx.Done():
		return ImageEvent{}, false, nil
	case err, ok := <-errs:
		if !ok {
			return ImageEvent{}, false, nil
		}
		return ImageEvent{}, true, err
	case event, ok := <-imageEvents:
		return event, ok, nil
	}
}

func drainAndClosePullStream(body io.ReadCloser) error {
	if body == nil {
		return nil
	}
	var err error
	if _, copyErr := io.Copy(io.Discard, body); copyErr != nil {
		err = errors.Join(err, oops.In("docker").Wrapf(copyErr, "read docker image pull stream"))
	}
	if closeErr := body.Close(); closeErr != nil {
		err = errors.Join(err, oops.In("docker").Wrapf(closeErr, "close docker image pull stream"))
	}
	return err
}

func dockerHostLabel(host string) string {
	if host == "" {
		return "default"
	}
	return host
}
