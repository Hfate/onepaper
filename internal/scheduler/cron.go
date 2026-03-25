package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/Hfate/onepaper/config"
	"github.com/Hfate/onepaper/pkg/logger"
	"github.com/robfig/cron/v3"
)

// StartCron 启动定时任务（默认可配置 cron 表达式与时区）。
func StartCron(cfg *config.Config, job func(context.Context) error) (*cron.Cron, error) {
	loc, err := time.LoadLocation(cfg.Scheduler.Timezone)
	if err != nil {
		return nil, fmt.Errorf("timezone: %w", err)
	}
	c := cron.New(cron.WithLocation(loc))
	_, err = c.AddFunc(cfg.Scheduler.Cron, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		logger.L.Info("scheduled job start", "cron", cfg.Scheduler.Cron)
		if err := job(ctx); err != nil {
			logger.L.Error("scheduled job failed", "err", err)
			return
		}
		logger.L.Info("scheduled job done")
	})
	if err != nil {
		return nil, err
	}
	c.Start()
	return c, nil
}
