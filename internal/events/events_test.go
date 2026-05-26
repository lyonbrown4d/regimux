package events_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/events"
)

type testEvent struct {
	Value int
}

func (testEvent) Name() string {
	return "events.test"
}

func TestNewSubscriberRegistersTypedHandler(t *testing.T) {
	bus := events.NewBus(testLogger())
	defer func() {
		if err := bus.Close(); err != nil {
			t.Fatalf("close bus: %v", err)
		}
	}()

	var got int
	subscriber := events.NewSubscriber(func(_ context.Context, event testEvent) error {
		got += event.Value
		return nil
	})

	unsubscribe, err := subscriber.Subscribe(bus)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := events.Publish(context.Background(), bus, testEvent{Value: 2}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	unsubscribe()
	if err := events.Publish(context.Background(), bus, testEvent{Value: 3}); err != nil {
		t.Fatalf("publish after unsubscribe: %v", err)
	}

	if got != 2 {
		t.Fatalf("handler calls = %d, want 2", got)
	}
}

func TestSubscriptionsRegisterAndClose(t *testing.T) {
	bus := events.NewBus(testLogger())
	defer func() {
		if err := bus.Close(); err != nil {
			t.Fatalf("close bus: %v", err)
		}
	}()

	var got int
	subscriptions := events.NewSubscriptions()
	subscribers := collectionlist.NewList[events.Subscriber](
		events.NewSubscriber(func(_ context.Context, event testEvent) error {
			got += event.Value
			return nil
		}),
	)

	if err := subscriptions.Register(bus, subscribers); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := events.Publish(context.Background(), bus, testEvent{Value: 1}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := subscriptions.Close(); err != nil {
		t.Fatalf("close subscriptions: %v", err)
	}
	if err := events.Publish(context.Background(), bus, testEvent{Value: 1}); err != nil {
		t.Fatalf("publish after close: %v", err)
	}

	if got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
}

func TestSubscriptionsRollBackOnRegisterError(t *testing.T) {
	bus := events.NewBus(testLogger())
	defer func() {
		if err := bus.Close(); err != nil {
			t.Fatalf("close bus: %v", err)
		}
	}()

	var got int
	wantErr := errors.New("boom")
	subscriptions := events.NewSubscriptions()
	subscribers := collectionlist.NewList[events.Subscriber](
		events.NewSubscriber(func(_ context.Context, event testEvent) error {
			got += event.Value
			return nil
		}),
		events.SubscriberFunc(func(events.Bus) (events.Unsubscribe, error) {
			return nil, wantErr
		}),
	)

	if err := subscriptions.Register(bus, subscribers); !errors.Is(err, wantErr) {
		t.Fatalf("register error = %v, want %v", err, wantErr)
	}
	if err := events.Publish(context.Background(), bus, testEvent{Value: 1}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got != 0 {
		t.Fatalf("handler calls = %d, want rollback to unsubscribe handler", got)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
