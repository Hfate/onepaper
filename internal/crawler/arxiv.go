package crawler

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Hfate/onepaper/internal/model"
	"github.com/Hfate/onepaper/pkg/logger"
)

// arXiv 建议在请求里标明用途；无 UA 时服务端偶发 500 或限流。
const arxivUserAgent = "onepaper/1.0 (+https://arxiv.org/help/bulk_data; academic aggregation)"

// Arxiv 使用 arXiv API（Atom）抓取最近提交的论文。
type Arxiv struct {
	HTTP          *http.Client
	MaxResults    int
	LookbackHours int
}

// Name 实现 Source。
func (a *Arxiv) Name() string { return "arxiv" }

// FetchRecent 获取最近 lookback 小时内提交的论文，最多 limit 条。
func (a *Arxiv) FetchRecent(ctx context.Context, limit int) ([]model.Paper, error) {
	if a.HTTP == nil {
		a.HTTP = &http.Client{Timeout: 90 * time.Second}
	}
	if a.LookbackHours <= 0 {
		a.LookbackHours = 24
	}
	if limit <= 0 {
		limit = 20
	}

	end := time.Now().UTC()
	start := end.Add(-time.Duration(a.LookbackHours) * time.Hour)

	papers, err := a.fetchBySubmittedRange(ctx, limit, start, end)
	if err != nil {
		logger.L.Warn("arxiv submittedDate query failed, using sorted fallback", "err", err)
		return a.fetchRecentSortedAndFilter(ctx, limit, start)
	}
	if len(papers) == 0 {
		logger.L.Info("arxiv submittedDate window empty, using sorted fallback")
		return a.fetchRecentSortedAndFilter(ctx, limit, start)
	}
	return papers, nil
}

func (a *Arxiv) fetchBySubmittedRange(ctx context.Context, limit int, start, end time.Time) ([]model.Paper, error) {
	from := start.Format("200601021504")
	to := end.Format("200601021504")
	q := fmt.Sprintf("submittedDate:[%s+TO+%s]", from, to)
	u := "https://export.arxiv.org/api/query?" + url.Values{
		"search_query": {q},
		"sortBy":         {"submittedDate"},
		"sortOrder":      {"descending"},
		"max_results":    {fmt.Sprintf("%d", limit)},
		"start":          {"0"},
	}.Encode()

	data, status, err := a.getWithRetry(ctx, u)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("arxiv status %d: %s", status, truncate(string(data), 800))
	}
	return parseAtomFeed(data, limit, a.Name(), start, false)
}

// fallbackSearchQuery 宽学科 OR（避免仅用 submittedDate 区间触发 arXiv 500）；结果再按时间窗本地过滤。
const fallbackSearchQuery = "(cat:cs OR cat:math OR cat:stat OR cat:physics OR cat:q-bio OR cat:eess OR cat:astro-ph OR cat:econ OR cat:gr-qc)"

// fetchRecentSortedAndFilter 使用宽搜索 + 按提交时间排序，本地裁剪到 lookback 窗口（不依赖 submittedDate 区间）。
func (a *Arxiv) fetchRecentSortedAndFilter(ctx context.Context, limit int, windowStart time.Time) ([]model.Paper, error) {
	fetchN := limit * 5
	if fetchN < 50 {
		fetchN = 50
	}
	if fetchN > 200 {
		fetchN = 200
	}
	u := "https://export.arxiv.org/api/query?" + url.Values{
		"search_query": {fallbackSearchQuery},
		"sortBy":       {"submittedDate"},
		"sortOrder":    {"descending"},
		"max_results":  {fmt.Sprintf("%d", fetchN)},
		"start":        {"0"},
	}.Encode()

	data, status, err := a.getWithRetry(ctx, u)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("arxiv fallback status %d: %s", status, truncate(string(data), 800))
	}
	return parseAtomFeed(data, limit, a.Name(), windowStart, true)
}

func (a *Arxiv) getWithRetry(ctx context.Context, rawURL string) ([]byte, int, error) {
	logger.L.Info("arxiv fetch", "url", rawURL)
	var lastErr error
	var lastStatus int
	var body []byte
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			wait := backoffDelay(attempt)
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(wait):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("User-Agent", arxivUserAgent)
		req.Header.Set("Accept", "application/atom+xml")

		resp, err := a.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastStatus = resp.StatusCode
		body = b

		if resp.StatusCode == http.StatusOK {
			if isArxivAPIErrorBody(b) {
				lastErr = fmt.Errorf("arxiv api error entry in feed")
				continue
			}
			// arXiv occasionally returns a truncated Atom feed (still 200),
			// which later fails XML parsing with "unexpected EOF".
			if looksTruncatedAtomFeed(b) {
				lastErr = fmt.Errorf("arxiv atom feed looks truncated")
				continue
			}
			return b, resp.StatusCode, nil
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("arxiv http %d", resp.StatusCode)
			if d := retryAfterDelay(resp.Header.Get("Retry-After")); d > 0 {
				select {
				case <-ctx.Done():
					return nil, 0, ctx.Err()
				case <-time.After(d):
				}
			}
			continue
		}
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("arxiv http %d", resp.StatusCode)
			continue
		}
		return b, resp.StatusCode, nil
	}
	if lastErr != nil {
		return body, lastStatus, fmt.Errorf("%w: last status %d", lastErr, lastStatus)
	}
	return body, lastStatus, fmt.Errorf("arxiv: exhausted retries, status %d", lastStatus)
}

func looksTruncatedAtomFeed(data []byte) bool {
	s := strings.TrimSpace(string(data))
	if s == "" {
		return true
	}
	// Most valid responses start with XML declaration or <feed>.
	if !strings.HasPrefix(s, "<?xml") && !strings.HasPrefix(s, "<feed") {
		return false
	}
	// A complete Atom feed must have a closing </feed>.
	return !strings.Contains(s, "</feed>")
}

func retryAfterDelay(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	// Retry-After: <seconds>
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		// add a small cushion
		return time.Duration(secs)*time.Second + 500*time.Millisecond
	}
	// Retry-After: HTTP-date
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t) + 500*time.Millisecond
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

func backoffDelay(attempt int) time.Duration {
	// Exponential backoff with a tiny deterministic jitter.
	// attempt starts at 1 for the first wait.
	base := 1500 * time.Millisecond
	if attempt < 1 {
		attempt = 1
	}
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= 30*time.Second {
			d = 30 * time.Second
			break
		}
	}
	// jitter in [0, 250ms)
	jitter := time.Duration(time.Now().UnixNano()%250_000_000) * time.Nanosecond
	return d + jitter
}

func isArxivAPIErrorBody(data []byte) bool {
	s := string(data)
	return strings.Contains(s, "arxiv.org/api/errors") && strings.Contains(s, "<title>Error</title>")
}

func parseAtomFeed(data []byte, limit int, source string, windowStart time.Time, filterWindow bool) ([]model.Paper, error) {
	var feed atomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, fmt.Errorf("arxiv atom parse: %w", err)
	}
	if isArxivAPIErrorEntry(feed.Entries) {
		return nil, fmt.Errorf("arxiv returned error entry in feed")
	}

	out := make([]model.Paper, 0, limit)
	for _, e := range feed.Entries {
		p, err := entryToPaper(e)
		if err != nil {
			logger.L.Warn("skip arxiv entry", "err", err)
			continue
		}
		if filterWindow {
			if p.Published.IsZero() || p.Published.Before(windowStart) {
				continue
			}
		}
		p.Source = source
		out = append(out, p)
		if len(out) >= limit {
			break
		}
	}
	logger.L.Info("arxiv done", "count", len(out), "filterWindow", filterWindow)
	return out, nil
}

func isArxivAPIErrorEntry(entries []atomEntry) bool {
	for _, e := range entries {
		if strings.Contains(e.ID, "arxiv.org/api/errors") || strings.EqualFold(strings.TrimSpace(e.Title), "Error") {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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
