package backend

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestDeletePrefixByScanEscapesGlobAndVerifiesPrefix(t *testing.T) {
	t.Parallel()

	var gotPattern string
	var deleted []string
	err := deletePrefixByScan(context.Background(), "tenant:manifest[1]*:",
		func(_ context.Context, cursor uint64, pattern string, count int64) ([]string, uint64, error) {
			if cursor != 0 || count != prefixDeleteBatchSize {
				t.Fatalf("scan cursor/count = %d/%d", cursor, count)
			}
			gotPattern = pattern
			return []string{
				"tenant:manifest[1]*:application/json",
				"tenant:manifest1X:application/json",
				"tenant:manifest[1]X:application/json",
			}, 0, nil
		},
		func(_ context.Context, keys []string) error {
			deleted = append(deleted, keys...)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("deletePrefixByScan: %v", err)
	}
	if want := `tenant:manifest\[1\]\*:*`; gotPattern != want {
		t.Fatalf("scan pattern = %q, want %q", gotPattern, want)
	}
	if want := []string{"tenant:manifest[1]*:application/json"}; !reflect.DeepEqual(deleted, want) {
		t.Fatalf("deleted keys = %q, want %q", deleted, want)
	}
}

func TestDeletePrefixByScanPropagatesScanAndDeleteErrors(t *testing.T) {
	t.Parallel()

	scanFailure := errors.New("scan failed")
	if err := deletePrefixByScan(context.Background(), "manifest:",
		func(context.Context, uint64, string, int64) ([]string, uint64, error) {
			return nil, 0, scanFailure
		}, func(context.Context, []string) error { return nil }); !errors.Is(err, scanFailure) {
		t.Errorf("scan error = %v, want original scan error", err)
	}

	deleteFailure := errors.New("delete failed")
	if err := deletePrefixByScan(context.Background(), "manifest:",
		func(context.Context, uint64, string, int64) ([]string, uint64, error) {
			return []string{"manifest:one"}, 0, nil
		}, func(context.Context, []string) error { return deleteFailure }); !errors.Is(err, deleteFailure) {
		t.Errorf("delete error = %v, want original delete error", err)
	}
}

func TestDeletePrefixByScanStopsWhenContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scanCalled := false
	err := deletePrefixByScan(ctx, "manifest:",
		func(context.Context, uint64, string, int64) ([]string, uint64, error) {
			scanCalled = true
			return nil, 0, nil
		}, func(context.Context, []string) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("deletePrefixByScan error = %v, want context.Canceled", err)
	}
	if scanCalled {
		t.Fatal("scan was called with an already canceled context")
	}
}

func TestDeletePrefixAcrossNodesScansEveryNodeWithIndependentCursors(t *testing.T) {
	t.Parallel()

	var firstCursors []uint64
	var secondCursors []uint64
	var deleted []string
	scanners := []keyScanner{
		func(_ context.Context, cursor uint64, _ string, _ int64) ([]string, uint64, error) {
			firstCursors = append(firstCursors, cursor)
			if cursor == 0 {
				return []string{"manifest:first"}, 17, nil
			}
			return nil, 0, nil
		},
		func(_ context.Context, cursor uint64, _ string, _ int64) ([]string, uint64, error) {
			secondCursors = append(secondCursors, cursor)
			if cursor == 0 {
				return []string{"manifest:second"}, 29, nil
			}
			return nil, 0, nil
		},
	}
	if err := deletePrefixAcrossNodes(context.Background(), "manifest:", scanners,
		func(_ context.Context, keys []string) error {
			deleted = append(deleted, keys...)
			return nil
		}); err != nil {
		t.Fatalf("deletePrefixAcrossNodes: %v", err)
	}
	if want := []uint64{0, 17}; !reflect.DeepEqual(firstCursors, want) {
		t.Errorf("first node cursors = %v, want %v", firstCursors, want)
	}
	if want := []uint64{0, 29}; !reflect.DeepEqual(secondCursors, want) {
		t.Errorf("second node cursors = %v, want %v", secondCursors, want)
	}
	if want := []string{"manifest:first", "manifest:second"}; !reflect.DeepEqual(deleted, want) {
		t.Errorf("deleted keys = %v, want %v", deleted, want)
	}
}

func TestDeletePrefixAcrossNodesRejectsEmptyNodeSet(t *testing.T) {
	t.Parallel()

	if err := deletePrefixAcrossNodes(context.Background(), "manifest:", nil,
		func(context.Context, []string) error { return nil }); !errors.Is(err, ErrNoCacheNodes) {
		t.Fatalf("empty node set error = %v, want ErrNoCacheNodes", err)
	}
}
