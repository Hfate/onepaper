package summarizer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Hfate/onepaper/internal/model"
	"github.com/Hfate/onepaper/pkg/ai"
	"github.com/Hfate/onepaper/pkg/logger"
)

// Generator 科普长文生成器。
type Generator struct {
	Client   *ai.Client
	Model    string
	MinWords int
	MaxWords int
}

type articleJSON struct {
	Title         string `json:"title"`
	CoreViewpoint string `json:"core_viewpoint"`
	Intro         string `json:"intro"`
	Sections      []struct {
		PaperID      string `json:"paper_id"`
		Heading      string `json:"heading"`
		ShortComment string `json:"short_comment"`
		Body         string `json:"body"`
	} `json:"sections"`
	Summary string `json:"summary"`
}

const articlePrompt = `You are a Chinese science writer for WeChat. Produce ONE cohesive popular-science article that covers EXACTLY the papers listed (one section per paper, same order as input).
Fixed structure (must follow):
1) title: catchy, accurate.
2) core_viewpoint: 2–4 sentences in Simplified Chinese stating the single main thesis of the whole piece (what readers should take away).
3) intro: opening hook (question or counterintuitive fact), leading into the topic; do not repeat core_viewpoint verbatim.
4) sections: one object per paper. Each must have:
   - paper_id: MUST match the paper's "id" from the input list exactly.
   - heading: section title for general readers.
   - short_comment: 1–3 short sentences: your brief judgment or takeaway on this paper (tone: calm, critical-friendly).
   - body: main explanation for lay readers (core idea, why it matters); no need to repeat short_comment verbatim.
5) summary: closing reflection or trend (1–3 short paragraphs).

Language: Simplified Chinese, vivid but accurate, no hype without basis.
Total length about %d–%d Chinese characters (not counting spaces), spread across intro, sections, and summary.

Return ONLY valid JSON with keys: title, core_viewpoint, intro, sections (array of {paper_id, heading, short_comment, body}), summary.

Papers (JSON array):
%s`

// GenerateArticle 根据 Top 论文生成整篇文章骨架（中文）。
func (g *Generator) GenerateArticle(ctx context.Context, papers []model.Paper) (model.Article, error) {
	if len(papers) == 0 {
		return model.Article{}, fmt.Errorf("no papers")
	}
	payload, err := json.Marshal(papersBrief(papers))
	if err != nil {
		return model.Article{}, err
	}
	minW, maxW := g.MinWords, g.MaxWords
	if minW <= 0 {
		minW = 1500
	}
	if maxW <= 0 {
		maxW = 2500
	}
	prompt := fmt.Sprintf(articlePrompt, minW, maxW, string(payload))
	req := ai.ChatRequest{
		Messages:    []ai.Message{{Role: "user", Content: prompt}},
		Temperature: 0.7,
		MaxTokens:   4096,
		ResponseFormat: &struct {
			Type string `json:"type"`
		}{Type: "json_object"},
	}
	raw, err := g.Client.ChatCompletion(ctx, g.Model, req)
	if err != nil {
		return model.Article{}, err
	}
	raw = stripJSONFence(raw)
	var aj articleJSON
	if err := json.Unmarshal([]byte(raw), &aj); err != nil {
		logger.L.Error("article json parse failed", "err", err, "raw", truncate(raw, 800))
		return model.Article{}, fmt.Errorf("parse article json: %w", err)
	}
	art := model.Article{
		Title:         strings.TrimSpace(aj.Title),
		CoreViewpoint: strings.TrimSpace(aj.CoreViewpoint),
		Intro:         strings.TrimSpace(aj.Intro),
		Summary:       strings.TrimSpace(aj.Summary),
	}
	for _, s := range aj.Sections {
		art.Sections = append(art.Sections, model.ArticleSection{
			PaperID:      strings.TrimSpace(s.PaperID),
			Heading:      strings.TrimSpace(s.Heading),
			ShortComment: strings.TrimSpace(s.ShortComment),
			Body:         strings.TrimSpace(s.Body),
		})
	}
	if art.Title == "" || art.CoreViewpoint == "" || len(art.Sections) == 0 {
		return model.Article{}, fmt.Errorf("incomplete article from model (need title, core_viewpoint, and sections)")
	}
	logger.L.Info("article generated", "title", art.Title, "sections", len(art.Sections))
	return art, nil
}

type briefPaper struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Abstract string `json:"abstract"`
}

func papersBrief(ps []model.Paper) []briefPaper {
	out := make([]briefPaper, 0, len(ps))
	for _, p := range ps {
		out = append(out, briefPaper{
			ID:       p.ID,
			Title:    truncate(p.Title, 300),
			Abstract: truncate(p.Abstract, 1200),
		})
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}
