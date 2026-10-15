package backend

import (
	"context"
	"time"
)

type Noop struct{}

var _ Backend = Noop{}
var _ PrefixDeleter = Noop{}

func (Noop) Get(context.Context, string) ([]byte, bool, error) {
	return nil, false, nil
}

func (Noop) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}

func (Noop) Delete(context.Context, string) error {
	return nil
}

func (Noop) DeletePrefix(_ context.Context, prefix string) error {
	return validateDeletePrefix(prefix)
}

func (Noop) Close() error {
	return nil
}
