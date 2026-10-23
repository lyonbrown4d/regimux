package worker

import (
	"errors"
	"fmt"
	"time"

	"github.com/panjf2000/ants/v2"
)

// SubmitAccess never queues or blocks; access timestamps are best-effort writes.
func (p *Pools) SubmitAccess(task func()) error {
	if task == nil {
		return errors.New("cache access task is required")
	}
	if p == nil || p.accessPool == nil {
		return fmt.Errorf("access worker pool is unavailable: %w", ants.ErrPoolClosed)
	}
	if err := p.accessPool.Submit(task); err != nil {
		return fmt.Errorf("submit access worker task: %w", err)
	}
	return nil
}

func (p *Pools) closeAccessPool() {
	if p.accessPool == nil || p.accessPool.IsClosed() {
		return
	}
	// Drain bounded timestamp writes before the metadata dependencies shut down.
	if err := p.accessPool.ReleaseTimeout(time.Second); err != nil {
		p.logger.Warn("close access worker pool failed", "error", err)
	}
}
