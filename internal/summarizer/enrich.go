package summarizer

import (
	"strings"

	"github.com/Hfate/onepaper/internal/model"
	"github.com/Hfate/onepaper/pkg/logger"
)

// EnrichArticleMeta 按 PaperID 将作者、原文链接写入各节（确定性数据，不由模型生成）。
func EnrichArticleMeta(article *model.Article, papers []model.Paper) {
	byID := make(map[string]model.Paper, len(papers))
	for _, p := range papers {
		id := strings.TrimSpace(p.ID)
		if id != "" {
			byID[id] = p
		}
	}
	for i := range article.Sections {
		sec := &article.Sections[i]
		pid := strings.TrimSpace(sec.PaperID)
		p, ok := byID[pid]
		if !ok {
			logger.L.Warn("article section paper_id not in batch", "paper_id", pid)
			continue
		}
		sec.AuthorsLine = formatAuthorsLine(p.Authors)
		sec.SourceURL = strings.TrimSpace(p.URL)
	}
}

// formatAuthorsLine 取前两位作者，多于两位则加「等」。
func formatAuthorsLine(authors []string) string {
	var names []string
	for _, a := range authors {
		a = strings.TrimSpace(a)
		if a != "" {
			names = append(names, a)
		}
	}
	if len(names) == 0 {
		return ""
	}
	if len(names) == 1 {
		return names[0]
	}
	if len(names) == 2 {
		return names[0] + "，" + names[1]
	}
	return names[0] + "，" + names[1] + " 等"
}
