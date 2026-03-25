package crawler

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Hfate/onepaper/internal/model"
	"github.com/Hfate/onepaper/pkg/logger"
)

// Arxiv 使用 arXiv API（Atom）抓取最近提交的论文。
type Arxiv struct {
	HTTP       *http.Client
	MaxResults int
	// LookbackHours 查询时间窗（按提交时间）。
	LookbackHours int
}

// Name 实现 Source。
func (a *Arxiv) Name() string { return "arxiv" }

// FetchRecent 获取最近 lookback 小时内提交的论文，最多 limit 条。
func (a *Arxiv) FetchRecent(ctx context.Context, limit int) ([]model.Paper, error) {
	if a.HTTP == nil {
		a.HTTP = &http.Client{Timeout: 60 * time.Second}
	}
	if a.LookbackHours <= 0 {
		a.LookbackHours = 24
	}
	if limit <= 0 {
		limit = 20
	}

	end := time.Now().UTC()
	start := end.Add(-time.Duration(a.LookbackHours) * time.Hour)
	// arXiv submittedDate 区间：YYYYMMDDHHMMSS（UTC）
	from := start.Format("200601021504")
	to := end.Format("200601021504")
	q := fmt.Sprintf("submittedDate:[%s+TO+%s]", from, to)
	u := "https://export.arxiv.org/api/query?" + url.Values{
		"search_query": {q},
		"sortBy":       {"submittedDate"},
		"sortOrder":    {"descending"},
		"max_results":  {fmt.Sprintf("%d", limit)},
		"start":        {"0"},
	}.Encode()

	logger.L.Info("arxiv fetch", "url", u)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("arxiv http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("arxiv status %d: %s", resp.StatusCode, string(b))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var feed atomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, fmt.Errorf("arxiv atom parse: %w", err)
	}

	out := make([]model.Paper, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		p, err := entryToPaper(e)
		if err != nil {
			logger.L.Warn("skip arxiv entry", "err", err)
			continue
		}
		p.Source = a.Name()
		out = append(out, p)
	}
	logger.L.Info("arxiv done", "count", len(out))
	return out, nil
}

// atomFeed 仅解析 arXiv Atom 所需字段（任意命名空间）。
type atomFeed struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID        string       `xml:"id"`
	Title     string       `xml:"title"`
	Summary   string       `xml:"summary"`
	Published string       `xml:"published"`
	Authors   []atomAuthor `xml:"author"`
	Links     []atomLink   `xml:"link"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

func entryToPaper(e atomEntry) (model.Paper, error) {
	id := extractArxivID(e.ID)
	if id == "" {
		return model.Paper{}, fmt.Errorf("empty arxiv id")
	}
	title := cleanText(e.Title)
	abs := cleanText(e.Summary)
	var authors []string
	for _, a := range e.Authors {
		t := cleanText(a.Name)
		if t != "" {
			authors = append(authors, t)
		}
	}
	pub, _ := time.Parse(time.RFC3339, strings.TrimSpace(e.Published))
	absURL := "https://arxiv.org/abs/" + id
	pdfURL := "https://arxiv.org/pdf/" + id + ".pdf"
	for _, l := range e.Links {
		if l.Rel == "alternate" && strings.Contains(l.Type, "pdf") {
			pdfURL = l.Href
		}
	}
	return model.Paper{
		ID:        id,
		Title:     title,
		Abstract:  abs,
		Authors:   authors,
		URL:       absURL,
		PDFURL:    pdfURL,
		Published: pub,
	}, nil
}

func extractArxivID(atomID string) string {
	atomID = strings.TrimSpace(atomID)
	// http://arxiv.org/abs/1234.5678v1
	if i := strings.LastIndex(atomID, "/abs/"); i >= 0 {
		s := atomID[i+len("/abs/"):]
		return strings.TrimSuffix(s, "/")
	}
	return atomID
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}
