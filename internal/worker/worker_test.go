package worker_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/worker"
)

type taskList []func(context.Context) error

func (l taskList) Len() int {
	return len(l)
}

func (l taskList) Range(fn func(index int, task func(context.Context) error) bool) {
	for index, task := range l {
		if !fn(index, task) {
			return
		}
	}
}

func TestRunAllSettledDoesNotCancelSiblingTasks(t *testing.T) {
	t.Parallel()

	secondStarted := make(chan struct{})
	firstFailed := make(chan struct{})
	tasks := taskList{
		func(context.Context) error {
			<-secondStarted
			close(firstFailed)
			return errors.New("first failure")
		},
		func(ctx context.Context) error {
			close(secondStarted)
			<-firstFailed
			time.Sleep(10 * time.Millisecond)
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("sibling context: %w", err)
			}
			return nil
		},
	}

	err := worker.RunAllSettled(context.Background(), nil, tasks)
	if err == nil || !strings.Contains(err.Error(), "first failure") {
		t.Fatalf("RunAllSettled error = %v, want first failure", err)
	}
	if strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("RunAllSettled canceled sibling context: %v", err)
	}
}
