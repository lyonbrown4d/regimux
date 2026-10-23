package pypi

import "time"

// The consumer lease is independent of per-attempt network idle timeouts. A
// blocked Read can recover across several attempts without being abandoned.
const packageReadLease = 30 * time.Second

// The caller cannot consume a body while Get records its pull. Keep request
// cancellation active, but start the consumer's idle window at response handoff.
func suspendResponseReadLease(resp *Response) func() {
	if resp == nil {
		return func() {}
	}
	body, ok := resp.Body.(*packageBody)
	if !ok {
		return func() {}
	}
	body.mu.Lock()
	body.pauseReadLease()
	body.mu.Unlock()
	return body.resumeReadLease
}

func (b *packageBody) resumeReadLease() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.armReadLease()
}

// armReadLease and pauseReadLease require b.mu. A zero deadline identifies an
// active Read or service-owned bookkeeping before response handoff.
func (b *packageBody) armReadLease() {
	if b.closed || b.abandoned {
		return
	}
	b.readDeadline = time.Now().Add(packageReadLease)
	if b.readLease == nil {
		b.readLease = time.AfterFunc(packageReadLease, b.expireReadLease)
		return
	}
	b.readLease.Reset(packageReadLease)
}

func (b *packageBody) pauseReadLease() {
	b.readDeadline = time.Time{}
	if b.readLease != nil {
		b.readLease.Stop()
	}
}

func (b *packageBody) expireReadLease() {
	b.mu.Lock()
	// A stopped timer may already have queued this callback. Recheck the current
	// deadline so a newer or active Read retains ownership of its spool and fill.
	if b.closed || b.abandoned || b.readDeadline.IsZero() || time.Now().Before(b.readDeadline) {
		b.mu.Unlock()
		return
	}
	b.abandoned = true
	b.mu.Unlock()
	if err := b.close(b.ctx); err != nil {
		b.service.logger.DebugContext(b.ctx, "close abandoned pypi body failed")
	}
}
