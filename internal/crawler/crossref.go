package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Hfate/onepaper/internal/model"
	"github.com/Hfate/onepaper/pkg/logger"
)

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// Crossref Crossref works 抓取来源（偏元数据 + DOI；PDF 交给 Unpaywall）。
type Crossref struct {
	HTTP          *http.Client
	MaxResults    int
	LookbackHours int
}

func (c *Crossref) Name() string { return "crossref" }

func (c *Crossref) FetchRecent(ctx context.Context, limit int) ([]model.Paper, error) {
	if c.LookbackHours <= 0 {
		c.LookbackHours = 24
	}
	if limit <= 0 {
		limit = 20
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 45 * time.Second}
	}

	end := time.Now().UTC()
	start := end.Add(-time.Duration(c.LookbackHours) * time.Hour)

	from := start.Format("2006-01-02")
	until := end.Format("2006-01-02")

	// Crossref 支持 filter 语法：from-pub-date / until-pub-date
	filter := fmt.Sprintf("from-pub-date:%s,until-pub-date:%s", from, until)

	rows := limit * 5
	if rows < 50 {
		rows = 50
	}
	if rows > 500 {
		rows = 500
	}

	offset := 0
	var out []model.Paper
	for len(out) < limit && offset < 2000 {
		u := "https://api.crossref.org/works?" + url.Values{
			"filter": []string{filter},
			"rows":   []string{fmt.Sprintf("%d", rows)},
			"offset": []string{fmt.Sprintf("%d", offset)},
			// 优先用出版日期排序；字段名在 Crossref 可用性上略有差异，但大多数情况下可用。
			"sort":  []string{"published-print"},
			"order": []string{"desc"},
		}.Encode()

		logger.L.Info("crossref fetch", "offset", offset, "url", u)
		body, status, err := getJSONWithRetryHTTP(ctx, c.HTTP, u, 3)
		if err != nil {
			logger.L.Warn("crossref request failed", "err", err)
			break
		}
		if status != http.StatusOK {
			logger.L.Warn("crossref bad status", "status", status, "body", truncate(string(body), 300))
			break
		}

		var resp struct {
			Message struct {
				Items []struct {
					DOI      string   `json:"DOI"`
					Title    []string `json:"title"`
					Abstract string   `json:"abstract"`
					URL      string   `json:"URL"`
					Author   []struct {
						Given  string `json:"given"`
						Family string `json:"family"`
					} `json:"author"`
					PublishedPrint struct {
						DateParts [][]int `json:"date-parts"`
					} `json:"published-print"`
					PublishedOnline struct {
						DateParts [][]int `json:"date-parts"`
					} `json:"published-online"`
				} `json:"items"`
			} `json:"message"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			logger.L.Warn("crossref json parse failed", "err", err)
			break
		}

		for _, it := range resp.Message.Items {
			title := strings.TrimSpace(firstNonEmpty(it.Title))
			if title == "" {
				continue
			}

			authors := make([]string, 0, len(it.Author))
			for _, a := range it.Author {
				g := strings.TrimSpace(a.Given)
				f := strings.TrimSpace(a.Family)
				if g == "" && f == "" {
					continue
				}
				if g == "" {
					authors = append(authors, f)
				} else if f == "" {
					authors = append(authors, g)
				} else {
					authors = append(authors, g+" "+f)
				}
			}

			abstract := strings.TrimSpace(cleanAbstract(it.Abstract))

			published := firstDate(it.PublishedOnline.DateParts)
			if published.IsZero() {
				published = firstDate(it.PublishedPrint.DateParts)
			}

			doi := strings.TrimSpace(it.DOI)
			paper := model.Paper{
				ID:        doi,
				Title:     title,
				Abstract:  abstract,
				Authors:   authors,
				URL:       strings.TrimSpace(it.URL),
				DOI:       doi,
				PDFURL:    "",
				Published: published,
				Source:    c.Name(),
			}
			if paper.ID == "" {
				// 没 DOI 就没法 Unpaywall 补；但 crossref 仍可能返回 URL。这里允许放入，让后续 MultiSource/Unpaywall 再裁剪。
				paper.ID = strings.TrimSpace(it.URL)
			}
			if paper.ID == "" {
				continue
			}

			out = append(out, paper)
			if len(out) >= limit {
				break
			}
		}

		offset += rows
		if len(resp.Message.Items) == 0 {
			break
		}
	}

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func firstNonEmpty(ss []string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func cleanAbstract(s string) string {
	s = html.UnescapeString(s)
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	return truncate(s, 2000)
}

func firstDate(parts [][]int) time.Time {
	if len(parts) == 0 || len(parts[0]) == 0 {
		return time.Time{}
	}
	dp := parts[0]
	year := dp[0]
	month := 1
	day := 1
	if len(dp) >= 2 && dp[1] >= 1 && dp[1] <= 12 {
		month = dp[1]
	}
	if len(dp) >= 3 && dp[2] >= 1 && dp[2] <= 31 {
		day = dp[2]
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func getJSONWithRetryHTTP(ctx context.Context, c *http.Client, rawURL string, attempts int) ([]byte, int, error) {
	var lastErr error
	var lastStatus int
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 400 * time.Millisecond)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("User-Agent", "onepaper/1.0")
		req.Header.Set("Accept", "application/json")

		resp, err := c.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastStatus = resp.StatusCode
		if resp.StatusCode == http.StatusOK {
			return b, resp.StatusCode, nil
		}
		lastErr = fmt.Errorf("http %d", resp.StatusCode)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			continue
		}
		return b, resp.StatusCode, lastErr
	}
	return nil, lastStatus, lastErr
}
