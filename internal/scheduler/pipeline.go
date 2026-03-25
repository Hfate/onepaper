package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/Hfate/onepaper/config"
	"github.com/Hfate/onepaper/internal/crawler"
	"github.com/Hfate/onepaper/internal/filter"
	"github.com/Hfate/onepaper/internal/image"
	"github.com/Hfate/onepaper/internal/model"
	"github.com/Hfate/onepaper/internal/publisher"
	"github.com/Hfate/onepaper/internal/repository"
	"github.com/Hfate/onepaper/internal/summarizer"
	"github.com/Hfate/onepaper/pkg/logger"
)

// Deps 流水线依赖注入。
type Deps struct {
	Config    *config.Config
	Crawler   crawler.Source
	Unpaywall *crawler.UnpaywallPDFResolver
	Scorer    *filter.Scorer
	Generator *summarizer.Generator
	Images    *image.Extractor
	Publisher *publisher.WeChatPublisher
	DB        *sql.DB
}

// RunOnce 执行完整流程：抓取 → 评分 → TopN → 成文 → 配图 → 存库 → 发布。
func RunOnce(ctx context.Context, d Deps) error {
	cfg := d.Config
	papers, err := d.Crawler.FetchRecent(ctx, cfg.Crawler.ArxivMaxResults)
	if err != nil {
		return fmt.Errorf("crawl: %w", err)
	}

	if d.Unpaywall != nil {
		papers, err = d.Unpaywall.Resolve(ctx, papers)
		if err != nil {
			return fmt.Errorf("unpaywall: %w", err)
		}
	}

	// fail_fast + require_pdf：在进入 AI/生成前过滤掉无法配图的论文。
	if cfg.RequirePDF() {
		filtered := papers[:0]
		for _, p := range papers {
			if strings.TrimSpace(p.PDFURL) != "" {
				filtered = append(filtered, p)
			}
		}
		papers = filtered
	}
	if len(papers) == 0 {
		if cfg.FailFast() {
			return fmt.Errorf("no papers with pdf available")
		}
		logger.L.Info("no papers in window, skip")
		return nil
	}

	type scored struct {
		p model.Paper
		s model.ScoreResult
	}
	var list []scored
	for _, p := range papers {
		sr, err := d.Scorer.ScorePaper(ctx, p)
		if err != nil {
			logger.L.Warn("score failed", "paper", p.ID, "err", err)
			continue
		}
		list = append(list, scored{p: p, s: sr})
		if d.DB != nil {
			sc := sr.Score
			if err := repository.SavePaper(ctx, d.DB, p, &sc, p.Source); err != nil {
				logger.L.Warn("save paper row failed", "paper", p.ID, "err", err)
			}
		}
	}
	if len(list) == 0 {
		return fmt.Errorf("no paper scored successfully")
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].s.Score > list[j].s.Score
	})
	topN := cfg.Filter.TopN
	if topN > len(list) {
		topN = len(list)
	}
	top := list[:topN]
	topPapers := make([]model.Paper, 0, len(top))
	for _, x := range top {
		topPapers = append(topPapers, x.p)
	}

	article, err := d.Generator.GenerateArticle(ctx, topPapers)
	if err != nil {
		return fmt.Errorf("generate article: %w", err)
	}
	summarizer.EnrichArticleMeta(&article, topPapers)

	for i := range article.Sections {
		sec := &article.Sections[i]
		p := pickPaper(sec.PaperID, topPapers, i)
		path, err := d.Images.ExtractMainImage(ctx, p)
		if err != nil {
			logger.L.Warn("extract image failed", "paper", p.ID, "err", err)
			continue
		}
		sec.LocalImage = path
	}

	if err := d.Publisher.Publish(ctx, &article, summarizer.RenderHTML); err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	if d.DB != nil {
		status := "draft"
		if cfg.WeChat.PublishMode == "publish" {
			status = "published"
		}
		id, err := repository.InsertArticle(ctx, d.DB, article.Title, article.HTML, status)
		if err != nil {
			logger.L.Warn("save article failed", "err", err)
		} else {
			logger.L.Info("article saved", "id", id, "status", status)
		}
	}
	return nil
}

func pickPaper(id string, papers []model.Paper, idx int) model.Paper {
	id = strings.TrimSpace(id)
	if id != "" {
		for _, p := range papers {
			if p.ID == id {
				return p
			}
		}
	}
	if idx < len(papers) {
		return papers[idx]
	}
	return papers[0]
}
