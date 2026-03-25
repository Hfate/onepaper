package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
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

	aiClient := ai.New(cfg.AI.BaseURL, cfg.AI.APIKey, cfg.AI.MaxRetries, cfg.AIRequestTimeout())

	var sources []crawler.Source
	for _, name := range cfg.Crawler.Sources {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "arxiv":
			sources = append(sources, &crawler.Arxiv{
				HTTP:          &http.Client{Timeout: 45 * time.Second},
				MaxResults:    cfg.Crawler.ArxivMaxResults,
				LookbackHours: cfg.Crawler.LookbackHours,
			})
		case "openalex":
			sources = append(sources, &crawler.OpenAlex{
				HTTP:          &http.Client{Timeout: 45 * time.Second},
				LookbackHours: cfg.Crawler.LookbackHours,
			})
		case "pubmed":
			sources = append(sources, &crawler.PubMed{
				HTTP:          &http.Client{Timeout: 45 * time.Second},
				LookbackHours: cfg.Crawler.LookbackHours,
			})
		case "crossref":
			sources = append(sources, &crawler.Crossref{
				HTTP:          &http.Client{Timeout: 45 * time.Second},
				LookbackHours: cfg.Crawler.LookbackHours,
			})
		case "semantic_scholar":
			sources = append(sources, &crawler.SemanticScholar{
				HTTP:          &http.Client{Timeout: 45 * time.Second},
				LookbackHours: cfg.Crawler.LookbackHours,
			})
		default:
			logger.L.Warn("unknown source", "source", name)
		}
	}
	if len(sources) == 0 {
		sources = append(sources, &crawler.Arxiv{
			HTTP:          &http.Client{Timeout: 45 * time.Second},
			MaxResults:    cfg.Crawler.ArxivMaxResults,
			LookbackHours: cfg.Crawler.LookbackHours,
		})
	}

	var crawlerSource crawler.Source
	if len(sources) == 1 {
		crawlerSource = sources[0]
	} else {
		crawlerSource = &crawler.MultiSource{Sources: sources}
	}

	var unpaywall *crawler.UnpaywallPDFResolver
	if strings.TrimSpace(cfg.Unpaywall.Email) != "" {
		unpaywall = &crawler.UnpaywallPDFResolver{
			Email:   cfg.Unpaywall.Email,
			BaseURL: cfg.Unpaywall.BaseURL,
			HTTP:    &http.Client{Timeout: 30 * time.Second},
		}
	}
	scorer := &filter.Scorer{Client: aiClient, Model: cfg.AI.ScoreModel}
	gen := &summarizer.Generator{
		Client:   aiClient,
		Model:    cfg.AI.ArticleModel,
		MinWords: cfg.Summarizer.MinWords,
		MaxWords: cfg.Summarizer.MaxWords,
	}
	img := &image.Extractor{
		Dir:         cfg.Image.Dir,
		MinW:        cfg.Image.MinWidth,
		MinH:        cfg.Image.MinHeight,
		PdfFallback: cfg.Image.PdfFallback,
	}
	img.HTTP = &http.Client{Timeout: cfg.ImageDownloadTimeout()}
	img.PDFHTTP = &http.Client{Timeout: cfg.ImagePDFDownloadTimeout()}

	pub := publisher.New(publisher.Config{
		AppID:          cfg.WeChat.AppID,
		AppSecret:      cfg.WeChat.AppSecret,
		Author:         cfg.WeChat.Author,
		PublishMode:    cfg.WeChat.PublishMode,
		Token:          cfg.WeChat.Token,
		EncodingAESKey: cfg.WeChat.EncodingAESKey,
		DefaultThumb:   cfg.WeChat.DefaultThumb,
	})

	deps := scheduler.Deps{
		Config:    cfg,
		Crawler:   crawlerSource,
		Unpaywall: unpaywall,
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
		logger.L.Info("pipeline finished (-run-once), exiting")
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
