package inventory

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-co-op/gocron/v2"
)

// FactsScheduler runs periodic CMDB Facts refresh & baseline drift inspection via gocron/v2 (T-M1-04).
type FactsScheduler struct {
	sched  gocron.Scheduler
	logger *slog.Logger
}

// NewFactsScheduler registers a periodic background job at interval invoking onTick(ctx).
func NewFactsScheduler(interval time.Duration, logger *slog.Logger, onTick func(ctx context.Context)) (*FactsScheduler, error) {
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	s, err := gocron.NewScheduler()
	if err != nil {
		return nil, err
	}
	_, err = s.NewJob(
		gocron.DurationJob(interval),
		gocron.NewTask(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if onTick != nil {
				onTick(ctx)
			}
		}),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
	)
	if err != nil {
		_ = s.Shutdown()
		return nil, err
	}
	return &FactsScheduler{sched: s, logger: logger}, nil
}

// Start begins executing scheduled background ticks.
func (f *FactsScheduler) Start() {
	if f != nil && f.sched != nil {
		f.sched.Start()
	}
}

// Shutdown gracefully halts scheduled ticks and waits for in-flight jobs.
func (f *FactsScheduler) Shutdown() error {
	if f == nil || f.sched == nil {
		return nil
	}
	return f.sched.Shutdown()
}
