package crawler

import (
	"context"

	"github.com/Hfate/onepaper/internal/model"
)

// Source 可扩展的论文抓取源（arXiv / Nature / Science 等）。
type Source interface {
	Name() string
	FetchRecent(ctx context.Context, limit int) ([]model.Paper, error)
}
