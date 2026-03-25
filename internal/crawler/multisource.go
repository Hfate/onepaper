package crawler

import (
	"context"
	"errors"
	"sort"

	"github.com/Hfate/onepaper/internal/model"
	"github.com/Hfate/onepaper/pkg/logger"
)

// MultiSource 聚合多个论文抓取源，统一返回最近的候选论文。
// 目前不做复杂的权重/分页，优先保证端到端可用与去重正确。
type MultiSource struct {
	Sources []Source
}

func (m *MultiSource) Name() string { return "multi" }

func (m *MultiSource) FetchRecent(ctx context.Context, limit int) ([]model.Paper, error) {
	if len(m.Sources) == 0 {
		return nil, errors.New("no sources configured")
	}
	if limit <= 0 {
		limit = 20
	}

	// 给每个 source 留一点冗余，便于去重后仍能凑够 limit。
	per := limit
	if len(m.Sources) > 1 {
		per = (limit * 2) / len(m.Sources)
		if per < 5 {
			per = 5
		}
	}

	var all []model.Paper
	seen := make(map[string]struct{}, limit*2)

	addPaper := func(srcName string, p model.Paper) {
		if p.Source == "" {
			p.Source = srcName
		}
		key := p.ID
		if key == "" {
			key = p.URL
		}
		if key == "" {
			key = p.Title
		}
		if key == "" {
			return
		}
		k := srcName + ":" + key
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		all = append(all, p)
	}

	for _, s := range m.Sources {
		srcName := s.Name()
		papers, err := s.FetchRecent(ctx, per)
		if err != nil {
			logger.L.Warn("source fetch failed", "source", srcName, "err", err)
			continue
		}
		for _, p := range papers {
			addPaper(srcName, p)
		}
	}

	if len(all) == 0 {
		return nil, errors.New("no papers from any source")
	}

	sort.Slice(all, func(i, j int) bool {
		ti := all[i].Published
		tj := all[j].Published
		if ti.IsZero() && !tj.IsZero() {
			return false
		}
		if !ti.IsZero() && tj.IsZero() {
			return true
		}
		return ti.After(tj)
	})

	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}
