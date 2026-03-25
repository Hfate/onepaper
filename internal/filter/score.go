package filter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Hfate/onepaper/internal/model"
	"github.com/Hfate/onepaper/pkg/ai"
	"github.com/Hfate/onepaper/pkg/logger"
)

// Scorer 使用 LLM 对论文打分。
type Scorer struct {
	Client *ai.Client
	Model  string
}

const scorePrompt = `You are a scientific editor. Score the paper for a general-audience newsletter.
Return ONLY valid JSON with keys: novelty, impact, public_interest (each 0-10 float), score (sum or weighted total 0-30), reason (one short sentence in English, <=200 chars).
Paper title: %s
Abstract: %s`

// ScorePaper 对单篇论文评分（控制输入长度以节省 token）。
func (s *Scorer) ScorePaper(ctx context.Context, p model.Paper) (model.ScoreResult, error) {
	title := truncateRunes(p.Title, 400)
	abs := truncateRunes(p.Abstract, 2000)
	prompt := fmt.Sprintf(scorePrompt, title, abs)
	req := ai.ChatRequest{
		Messages: []ai.Message{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.2,
		MaxTokens:   300,
		ResponseFormat: &struct {
			Type string `json:"type"`
		}{Type: "json_object"},
	}
	out, err := s.Client.ChatCompletion(ctx, s.Model, req)
	if err != nil {
		return model.ScoreResult{}, err
	}
	// 兼容模型在 JSON 外包裹 markdown
	out = stripJSONFence(out)
	var r model.ScoreResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		logger.L.Warn("score json parse failed, retrying loose", "err", err, "raw", truncateRunes(out, 400))
		return model.ScoreResult{}, fmt.Errorf("parse score json: %w", err)
	}
	normalizeScores(&r)
	logger.L.Info("paper scored", "id", p.ID, "score", r.Score)
	return r, nil
}

func normalizeScores(r *model.ScoreResult) {
	clamp := func(x *float64) {
		if *x < 0 {
			*x = 0
		}
		if *x > 10 {
			*x = 10
		}
	}
	clamp(&r.Novelty)
	clamp(&r.Impact)
	clamp(&r.PublicInterest)
	if r.Score <= 0 {
		r.Score = r.Novelty + r.Impact + r.PublicInterest
	}
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

func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
