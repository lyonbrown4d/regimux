package container

import "context"

func (e *RegistryEndpoint) submitManifestBlobFill(ctx context.Context, work *manifestBlobFillWork, release func()) {
	submitted := false
	defer func() {
		if !submitted {
			work.clear()
			release()
		}
	}()
	pool := e.workers.IOPool()
	if pool == nil {
		e.logSkippedManifestBlobFill(ctx, work.route, work.digest, work.mediaType, "worker pool unavailable")
		return
	}
	if pool.Free() == 0 {
		e.logSkippedManifestBlobFill(ctx, work.route, work.digest, work.mediaType, "worker pool saturated")
		return
	}
	taskCtx := context.WithoutCancel(ctx)
	if err := pool.Submit(func() { work.run(taskCtx, release) }); err != nil {
		e.publishWorkerFillSkipped(ctx, work.route, "blob", "submit failed")
		e.logger.DebugContext(ctx, "submit manifest blob fill failed",
			"alias", work.route.Alias,
			"repository", work.route.Repo,
			"reference", work.route.Reference,
			"digest", work.digest,
			"error", err,
		)
		return
	}
	submitted = true
}
