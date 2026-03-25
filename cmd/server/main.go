package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Hfate/onepaper/config"
	"github.com/Hfate/onepaper/internal/crawler"
	"github.com/Hfate/onepaper/internal/filter"
	"github.com/Hfate/onepaper/internal/image"
	"github.com/Hfate/onepaper/internal/publisher"
	"github.com/Hfate/onepaper/internal/repository"
	"github.com/Hfate/onepaper/internal/scheduler"
	"github.com/Hfate/onepaper/internal/summarizer"
	"github.com/Hfate/onepaper/pkg/ai"
	"github.com/Hfate/onepaper/pkg/logger"
	"github.com/robfig/cron/v3"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	runOnce := flag.Bool("run-once", false, "run pipeline once and exit (no cron)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	var db *sql.DB
	if cfg.Database.DSN != "" {
		db, err = repository.OpenMySQL(cfg.Database.DSN)
		if err != nil {
			logger.L.Error("mysql", "err", err)
			os.Exit(1)
		}
		defer db.Close()
	}

	aiClient := ai.New(cfg.AI.BaseURL, cfg.AI.APIKey, cfg.AI.MaxRetries)
	arxiv := &crawler.Arxiv{
		MaxResults:    cfg.Crawler.ArxivMaxResults,
		LookbackHours: cfg.Crawler.LookbackHours,
	}
	scorer := &filter.Scorer{Client: aiClient, Model: cfg.AI.ScoreModel}
	gen := &summarizer.Generator{
		Client:   aiClient,
		Model:    cfg.AI.ArticleModel,
		MinWords: cfg.Summarizer.MinWords,
		MaxWords: cfg.Summarizer.MaxWords,
	}
	img := &image.Extractor{
		Dir:  cfg.Image.Dir,
		MinW: cfg.Image.MinWidth,
		MinH: cfg.Image.MinHeight,
	}
	img.HTTP = &http.Client{Timeout: cfg.ImageDownloadTimeout()}

	pub := publisher.New(publisher.Config{
		AppID:          cfg.WeChat.AppID,
		AppSecret:      cfg.WeChat.AppSecret,
		Author:         cfg.WeChat.Author,
		PublishMode:    cfg.WeChat.PublishMode,
		Token:          cfg.WeChat.Token,
		EncodingAESKey: cfg.WeChat.EncodingAESKey,
	})

	deps := scheduler.Deps{
		Config:    cfg,
		Arxiv:     arxiv,
		Scorer:    scorer,
		Generator: gen,
		Images:    img,
		Publisher: pub,
		DB:        db,
	}

	job := func(ctx context.Context) error {
		return scheduler.RunOnce(ctx, deps)
	}

	if *runOnce {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		if err := job(ctx); err != nil {
			logger.L.Error("pipeline", "err", err)
			os.Exit(1)
		}
		return
	}

	var cr *cron.Cron
	if cfg.Scheduler.Enabled {
		var err error
		cr, err = scheduler.StartCron(cfg, job)
		if err != nil {
			logger.L.Error("cron", "err", err)
			os.Exit(1)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	pub.RegisterPushHandler(mux, cfg.WeChat.PushPath)
	srv := &http.Server{Addr: cfg.Server.Addr, Handler: mux}

	go func() {
		logger.L.Info("http listening", "addr", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.L.Error("http server", "err", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	if cr != nil {
		cr.Stop()
	}
}
