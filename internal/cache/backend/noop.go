package backend

import (
	"context"
	"time"
)

type Noop struct{}

var _ Backend = Noop{}

func (Noop) Get(context.Context, string) ([]byte, bool, error) {
	return nil, false, nil
}

func (Noop) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}

func (Noop) Delete(context.Context, string) error {
	return nil
}

func (Noop) Close() error {
	return nil
}
