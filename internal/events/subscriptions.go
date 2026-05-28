package events

import (
	"context"
	"log/slog"
	"sync"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/eventx"
	"github.com/samber/oops"
)

type SubscribeOption = eventx.SubscribeOption
type Handler[T Event] func(context.Context, T) error
type Unsubscribe func()

type Subscriber interface {
	Subscribe(Bus) (Unsubscribe, error)
}

type SubscriberFunc func(Bus) (Unsubscribe, error)

func (f SubscriberFunc) Subscribe(bus Bus) (Unsubscribe, error) {
	if f == nil {
		return noopUnsubscribe, nil
	}
	return f(bus)
}

func NewSubscriber[T Event](handler Handler[T], options ...SubscribeOption) Subscriber {
	return SubscriberFunc(func(bus Bus) (Unsubscribe, error) {
		if handler == nil {
			return noopUnsubscribe, nil
		}
		unsubscribe, err := eventx.Subscribe[T](bus, handler, options...)
		if err != nil {
			return nil, oops.Wrapf(err, "subscribe event handler")
		}
		return unsubscribe, nil
	})
}

type Subscriptions struct {
	mu      sync.Mutex
	closed  bool
	entries *collectionlist.List[Unsubscribe]
}

func NewSubscriptions() *Subscriptions {
	return &Subscriptions{entries: collectionlist.NewList[Unsubscribe]()}
}

func NewLifecycleLogSubscriber(logger *slog.Logger) Subscriber {
	if logger == nil {
		logger = slog.Default()
	}
	return SubscriberFunc(func(bus Bus) (Unsubscribe, error) {
		return subscribeMany(
			bus,
			subscribe[ApplicationStarting](bus, func(ctx context.Context, event ApplicationStarting) error {
				logger.InfoContext(ctx, "application starting", "version", event.Version)
				return nil
			}),
			subscribe[ApplicationStarted](bus, func(ctx context.Context, event ApplicationStarted) error {
				logger.InfoContext(ctx, "application started", "version", event.Version)
				return nil
			}),
			subscribe[ApplicationStopping](bus, func(ctx context.Context, event ApplicationStopping) error {
				logger.InfoContext(ctx, "application stopping", "version", event.Version)
				return nil
			}),
			subscribe[ApplicationStopped](bus, func(ctx context.Context, event ApplicationStopped) error {
				logger.InfoContext(ctx, "application stopped", "version", event.Version)
				return nil
			}),
		)
	})
}

func (s *Subscriptions) Register(bus Bus, subscribers *collectionlist.List[Subscriber]) error {
	if s == nil || bus == nil || subscribers == nil || subscribers.Len() == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return oops.In("events").Errorf("event subscriptions are closed")
	}

	var registerErr error
	subscribers.Range(func(_ int, subscriber Subscriber) bool {
		if subscriber == nil {
			return true
		}
		unsubscribe, err := subscriber.Subscribe(bus)
		if err != nil {
			registerErr = oops.In("events").Wrapf(err, "subscribe event handler")
			return false
		}
		if unsubscribe != nil {
			s.entries.Add(unsubscribe)
		}
		return true
	})
	if registerErr != nil {
		s.closeLocked()
		return registerErr
	}
	return nil
}

func (s *Subscriptions) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked()
	return nil
}

func (s *Subscriptions) closeLocked() {
	if s.closed {
		return
	}
	s.closed = true
	if s.entries == nil {
		return
	}

	unsubscribeAll(s.entries)
	s.entries = collectionlist.NewList[Unsubscribe]()
}

func subscribe[T Event](bus Bus, handler func(context.Context, T) error) func() (Unsubscribe, error) {
	return func() (Unsubscribe, error) {
		unsubscribe, err := eventx.Subscribe[T](bus, handler)
		if err != nil {
			return nil, err
		}
		return unsubscribe, nil
	}
}

func subscribeMany(bus Bus, subscribers ...func() (Unsubscribe, error)) (Unsubscribe, error) {
	if bus == nil {
		return noopUnsubscribe, nil
	}
	unsubscribers := collectionlist.NewListWithCapacity[Unsubscribe](len(subscribers))
	for _, subscriber := range subscribers {
		if subscriber == nil {
			continue
		}
		unsubscribe, err := subscriber()
		if err != nil {
			unsubscribeAll(unsubscribers)
			return nil, oops.In("events").Wrapf(err, "subscribe event handler")
		}
		if unsubscribe != nil {
			unsubscribers.Add(unsubscribe)
		}
	}
	return func() {
		unsubscribeAll(unsubscribers)
	}, nil
}

func unsubscribeAll(unsubscribers *collectionlist.List[Unsubscribe]) {
	unsubscribers.Clone().Reverse().Range(func(_ int, unsubscribe Unsubscribe) bool {
		if unsubscribe != nil {
			unsubscribe()
		}
		return true
	})
}

func noopUnsubscribe() {}
