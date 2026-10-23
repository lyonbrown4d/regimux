package cache

import (
	"context"
	"testing"
	"time"

	"github.com/panjf2000/ants/v2"
)

func TestBlobFillLeaseCapacityDoesNotBlockAnotherOwner(t *testing.T) {
	pool, err := ants.NewPool(1)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Release()
	proxy := blobProxy{leaseScheduler: pool}
	firstStop := proxy.startBlobFillLease(context.Background(), BlobRequest{}, &recordingLease{})
	defer firstStop()
	started := make(chan func(), 1)
	go func() {
		started <- proxy.startBlobFillLease(context.Background(), BlobRequest{}, &recordingLease{})
	}()
	select {
	case stop := <-started:
		stop()
	case <-time.After(time.Second):
		firstStop()
		select {
		case stop := <-started:
			stop()
		case <-time.After(3 * time.Second):
			t.Error("second renewal remained blocked after first owner released")
		}
		t.Fatal("lease renewal capacity blocked another blob fill owner")
	}
}
