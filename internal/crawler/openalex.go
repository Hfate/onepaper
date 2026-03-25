package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Hfate/onepaper/internal/model"
	"github.com/Hfate/onepaper/pkg/logger"
)

const openAlexUserAgent = "onepaper/1.0 (https://example.com; academic aggregation)"

// OpenAlex OpenAlex works 抓取来源（偏通用元数据）。
type OpenAlex struct {
	HTTP          *http.Client
	MaxResults    int
	LookbackHours int
}

func (o *OpenAlex) Name() string { return "openalex" }

type openAlexWork struct {
	ID    string   `json:"id"`
	Title []string `json:"title"`
	DOI   string   `json:"doi"`

	Authorships []struct {
		Author struct {
			DisplayName string `json:"display_name"`
		} `json:"author"`
	} `json:"authorships"`

	AbstractInvertedIndex map[string][]int `json:"abstract_inverted_index"`
	PublicationDate       string           `json:"publication_date"`
	PrimaryLocation       struct {
		LandingPageURL string `json:"landing_page_url"`
	} `json:"primary_location"`
	OpenAccessLocations []struct {
		PDFURL string `json:"pdf_url"`
		URL    string `json:"url"`
	} `json:"open_access_locations"`
}

func (o *OpenAlex) FetchRecent(ctx context.Context, limit int) ([]model.Paper, error) {
	if o.LookbackHours <= 0 {
		o.LookbackHours = 24
	}
	if limit <= 0 {
		limit = 20
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 45 * time.Second}
	}

	end := time.Now().UTC()
	start := end.Add(-time.Duration(o.LookbackHours) * time.Hour)
	startDate := start.Format("2006-01-02")
	endDate := end.Format("2006-01-02")

	// 以发布时间窗抓取最近文章；并限定 journal articles（避免太多杂项）。
	filter := fmt.Sprintf("from_publication_date:%s,to_publication_date:%s,type:journal-article", startDate, endDate)

	perPage := limit * 5
	if perPage < 50 {
		perPage = 50
	}
	if perPage > 200 {
		perPage = 200
	}

	var out []model.Paper
	need := limit
	for page := 1; page <= 3 && len(out) < need; page++ {
		u := "https://api.openalex.org/works?" + url.Values{
			"filter":   []string{filter},
			"sort":     []string{"publication_date:desc"},
			"per-page": []string{fmt.Sprintf("%d", perPage)},
			"page":     []string{fmt.Sprintf("%d", page)},
		}.Encode()

		logger.L.Info("openalex fetch", "page", page, "url", u)
		body, status, err := getJSONWithRetry(ctx, o.HTTP, u, 3)
		if err != nil {
			logger.L.Warn("openalex request failed", "page", page, "err", err)
			continue
		}
		if status != http.StatusOK {
			logger.L.Warn("openalex bad status", "page", page, "status", status, "body", truncate(string(body), 400))
			continue
		}

		var resp struct {
			Results []openAlexWork `json:"results"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			logger.L.Warn("openalex json parse failed", "page", page, "err", err)
			continue
		}

		for _, w := range resp.Results {
			p := paperFromOpenAlexWork(w)
			if p.ID == "" || p.Title == "" {
				continue
			}
			out = append(out, p)
			if len(out) >= need {
				break
			}
		}
	}

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func getJSONWithRetry(ctx context.Context, httpClient *http.Client, rawURL string, attempts int) ([]byte, int, error) {
	var lastErr error
	var lastStatus int
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("User-Agent", openAlexUserAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := httpClient.Do(req)
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
	if lastErr == nil {
		lastErr = fmt.Errorf("request failed")
	}
	return nil, lastStatus, lastErr
}

func paperFromOpenAlexWork(w openAlexWork) model.Paper {
	_ = w
	p := model.Paper{}

	id := strings.TrimSpace(w.ID)
	if id != "" {
		if i := strings.LastIndex(id, "/"); i >= 0 {
			id = id[i+1:]
		}
	}

	title := ""
	if len(w.Title) > 0 {
		title = strings.TrimSpace(w.Title[0])
	}

	var authors []string
	for _, a := range w.Authorships {
		name := strings.TrimSpace(a.Author.DisplayName)
		if name != "" {
			authors = append(authors, name)
		}
	}

	abstract := reconstructAbstractFromInvertedIndex(w.AbstractInvertedIndex)
	abstract = truncate(abstract, 2000)

	published := parseOpenAlexDate(w.PublicationDate)

	url := strings.TrimSpace(w.PrimaryLocation.LandingPageURL)
	if url == "" && strings.TrimSpace(w.DOI) != "" {
		url = "https://doi.org/" + strings.TrimSpace(w.DOI)
	}

	pdfURL := ""
	for _, loc := range w.OpenAccessLocations {
		if strings.TrimSpace(loc.PDFURL) != "" {
			pdfURL = loc.PDFURL
			break
		}
		if strings.TrimSpace(loc.URL) != "" && strings.Contains(strings.ToLower(loc.URL), ".pdf") {
			pdfURL = loc.URL
			break
		}
	}

	p.ID = id
	p.Title = title
	p.Abstract = abstract
	p.Authors = authors
	p.URL = url
	p.DOI = strings.TrimSpace(w.DOI)
	p.PDFURL = pdfURL
	p.Published = published
	p.Source = "openalex"
	return p
}

func reconstructAbstractFromInvertedIndex(inv map[string][]int) string {
	if len(inv) == 0 {
		return ""
	}
	maxPos := -1
	for _, positions := range inv {
		for _, pos := range positions {
			if pos > maxPos {
				maxPos = pos
			}
		}
	}
	if maxPos < 0 {
		return ""
	}
	tokens := make([]string, maxPos+1)
	for tok, positions := range inv {
		tok = strings.TrimSpace(tok)
		for _, pos := range positions {
			if pos >= 0 && pos < len(tokens) {
				tokens[pos] = tok
			}
		}
	}
	return strings.Join(tokens, " ")
}

func parseOpenAlexDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	// OpenAlex 常见形如 "2026-03-24"
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	// 兜底：只取前 10 位
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t
		}
	}
	return time.Time{}
}
