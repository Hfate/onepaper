package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Hfate/onepaper/internal/model"
	"github.com/Hfate/onepaper/pkg/logger"
)

// SemanticScholar Semantic Scholar 抓取来源（偏元数据 + DOI；PDF 留给 Unpaywall）。
type SemanticScholar struct {
	HTTP          *http.Client
	MaxResults    int
	LookbackHours int
	// 作为检索 query 使用；用于缩小结果范围（避免全库抓取）。
	Query string
}

func (s *SemanticScholar) Name() string { return "semantic_scholar" }

func (s *SemanticScholar) FetchRecent(ctx context.Context, limit int) ([]model.Paper, error) {
	if s.LookbackHours <= 0 {
		s.LookbackHours = 24
	}
	if limit <= 0 {
		limit = 20
	}
	if s.HTTP == nil {
		s.HTTP = &http.Client{Timeout: 45 * time.Second}
	}
	if strings.TrimSpace(s.Query) == "" {
		// 默认用一个宽泛但不至于“全库”太大语义的 query。
		s.Query = "science"
	}

	end := time.Now().UTC()
	start := end.Add(-time.Duration(s.LookbackHours) * time.Hour)

	startDate := start.Format("2006-01-02")
	endDate := end.Format("2006-01-02")
	year := fmt.Sprintf("%d", end.Year())

	perPage := limit
	if perPage > 100 {
		perPage = 100
	}
	if perPage < 10 {
		perPage = 10
	}

	var out []model.Paper
	seen := make(map[string]struct{}, limit*2)

	offset := 0
	for len(out) < limit && offset < 2000 {
		u := "https://api.semanticscholar.org/graph/v1/paper/search?" + url.Values{
			"query":                    []string{s.Query},
			"year":                     []string{year},
			"publication_date_or_year": []string{startDate + "," + endDate},
			"offset":                   []string{fmt.Sprintf("%d", offset)},
			"limit":                    []string{fmt.Sprintf("%d", perPage)},
			// 返回必要字段；paperId 会默认包含在内
			"fields": []string{strings.Join([]string{
				"title",
				"abstract",
				"year",
				"publicationDate",
				"authors",
				"url",
				"externalIds",
				"doi",
			}, ",")},
		}.Encode()

		logger.L.Info("semanticscholar fetch", "offset", offset, "url", u)
		body, status, err := getJSONWithRetryHTTP(ctx, s.HTTP, u, 3)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			logger.L.Warn("semanticscholar bad status", "status", status, "body", truncate(string(body), 300))
			break
		}

		var resp struct {
			Data []struct {
				PaperID         string `json:"paperId"`
				Title           string `json:"title"`
				Abstract        string `json:"abstract"`
				Year            int    `json:"year"`
				PublicationDate string `json:"publicationDate"`
				Authors         []struct {
					Name string `json:"name"`
				} `json:"authors"`
				URL         string            `json:"url"`
				ExternalIds map[string]string `json:"externalIds"`
				DOI         string            `json:"doi"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			logger.L.Warn("semanticscholar json parse failed", "err", err)
			break
		}

		for _, p := range resp.Data {
			id := strings.TrimSpace(p.PaperID)
			if id == "" {
				// fallback: DOI 或 URL
				if strings.TrimSpace(p.DOI) != "" {
					id = strings.TrimSpace(p.DOI)
				} else {
					id = strings.TrimSpace(p.URL)
				}
			}
			if id == "" {
				continue
			}
			key := id + ":" + s.Name()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}

			doi := strings.TrimSpace(p.DOI)
			if doi == "" {
				doi = pickDOIFromExternalIds(p.ExternalIds)
			}

			authors := make([]string, 0, len(p.Authors))
			for _, a := range p.Authors {
				name := strings.TrimSpace(a.Name)
				if name != "" {
					authors = append(authors, name)
				}
			}

			published := time.Time{}
			if strings.TrimSpace(p.PublicationDate) != "" {
				published = parseMaybeDate(p.PublicationDate)
			}
			if published.IsZero() && p.Year > 0 {
				published = time.Date(p.Year, 1, 1, 0, 0, 0, 0, time.UTC)
			}

			out = append(out, model.Paper{
				ID:        id,
				Title:     strings.TrimSpace(p.Title),
				Abstract:  strings.TrimSpace(p.Abstract),
				Authors:   authors,
				URL:       strings.TrimSpace(p.URL),
				DOI:       doi,
				PDFURL:    "",
				Published: published,
				Source:    s.Name(),
			})
			if len(out) >= limit {
				break
			}
		}

		offset += perPage
		if len(resp.Data) == 0 {
			break
		}
	}

	// 只做一个粗排序（MultiSource 也会再排序）；Published 为空的放后面。
	sort.Slice(out, func(i, j int) bool {
		ti := out[i].Published
		tj := out[j].Published
		if ti.IsZero() && !tj.IsZero() {
			return false
		}
		if !ti.IsZero() && tj.IsZero() {
			return true
		}
		return ti.After(tj)
	})

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func pickDOIFromExternalIds(m map[string]string) string {
	for k, v := range m {
		if strings.EqualFold(k, "DOI") || strings.EqualFold(k, "doi") {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseMaybeDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t
		}
	}
	return time.Time{}
}
