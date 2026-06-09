package scheduler

import (
	"context"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/samber/oops"
)

type schedulerJobOptions struct {
	name               string
	tags               []string
	ctx                context.Context
	distributed        *bool
	startImmediately   bool
	limitedRuns        uint
	singletonLimitMode gocron.LimitMode
}

func buildJobOptions(cfg schedulerJobOptions) []gocron.JobOption {
	limitMode := cfg.singletonLimitMode
	if limitMode == 0 {
		limitMode = gocron.LimitModeReschedule
	}

	options := []gocron.JobOption{
		gocron.WithName(cfg.name),
		gocron.WithTags(cfg.tags...),
	}
	if cfg.ctx != nil {
		options = append(options, gocron.WithContext(cfg.ctx))
	}
	options = append(options, gocron.WithSingletonMode(limitMode))
	if cfg.distributed != nil {
		options = append(options, gocron.WithDisabledDistributedJobLocker(!*cfg.distributed))
	}
	if cfg.startImmediately {
		options = append(options, gocron.WithStartAt(gocron.WithStartImmediately()))
	}
	if cfg.limitedRuns > 0 {
		options = append(options, gocron.WithLimitedRuns(cfg.limitedRuns))
	}
	return options
}

func registerDurationJob(
	scheduler gocron.Scheduler,
	interval time.Duration,
	task func(context.Context) error,
	options schedulerJobOptions,
) (gocron.Job, error) {
	return registerGocronJob(scheduler, gocron.DurationJob(interval), task, options)
}

func registerImmediateJob(
	scheduler gocron.Scheduler,
	task func(context.Context) error,
	options schedulerJobOptions,
) (gocron.Job, error) {
	return registerGocronJob(scheduler, gocron.OneTimeJob(gocron.OneTimeJobStartImmediately()), task, options)
}

func registerGocronJob(
	scheduler gocron.Scheduler,
	definition gocron.JobDefinition,
	task func(context.Context) error,
	options schedulerJobOptions,
) (gocron.Job, error) {
	job, err := scheduler.NewJob(definition, gocron.NewTask(task), buildJobOptions(options)...)
	if err != nil {
		return nil, oops.Wrapf(err, "register gocron job")
	}
	return job, nil
}
