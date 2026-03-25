package crawler

import (
	"context"
	"encoding/json"
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

// PubMed PubMed 抓取来源（偏元数据+DOI；PDF 通过 Unpaywall 补齐）。
type PubMed struct {
	HTTP          *http.Client
	MaxResults    int
	LookbackHours int
}

func (p *PubMed) Name() string { return "pubmed" }

type pubMedDate struct {
	Year  string
	Month string
	Day   string
}

func (p *PubMed) FetchRecent(ctx context.Context, limit int) ([]model.Paper, error) {
	if p.LookbackHours <= 0 {
		p.LookbackHours = 24
	}
	if limit <= 0 {
		limit = 20
	}
	if p.HTTP == nil {
		p.HTTP = &http.Client{Timeout: 45 * time.Second}
	}

	end := time.Now().UTC()
	start := end.Add(-time.Duration(p.LookbackHours) * time.Hour)

	// PubMed 使用 PDAT（publication date）字段做时间窗。
	mindate := start.Format("2006/01/02")
	maxdate := end.Format("2006/01/02")
	term := fmt.Sprintf("(\"%s\"[PDAT] : \"%s\"[PDAT])", mindate, maxdate)

	retmax := limit * 2
	if retmax < 20 {
		retmax = 20
	}

	esearchURL := "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi?" + url.Values{
		"db":       []string{"pubmed"},
		"term":     []string{term},
		"retmax":   []string{strconv.Itoa(retmax)},
		"retmode":  []string{"json"},
		"datetype": []string{"pdat"},
	}.Encode()

	body, err := httpGetBytes(ctx, p.HTTP, esearchURL)
	if err != nil {
		return nil, err
	}

	var searchResp struct {
		ESearchResult struct {
			IDList []string `json:"idlist"`
		} `json:"esearchresult"`
	}
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, fmt.Errorf("pubmed esearch parse: %w", err)
	}
	if len(searchResp.ESearchResult.IDList) == 0 {
		return nil, nil
	}

	ids := searchResp.ESearchResult.IDList
	var out []model.Paper
	for i := 0; i < len(ids) && len(out) < limit; i += 25 {
		j := i + 25
		if j > len(ids) {
			j = len(ids)
		}
		batch := strings.Join(ids[i:j], ",")

		efetchURL := "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/efetch.fcgi?" + url.Values{
			"db":      []string{"pubmed"},
			"id":      []string{batch},
			"retmode": []string{"xml"},
			"rettype": []string{"abstract"},
		}.Encode()

		xmlBytes, err := httpGetBytes(ctx, p.HTTP, efetchURL)
		if err != nil {
			logger.L.Warn("pubmed efetch failed", "err", err)
			continue
		}
		papers, err := parsePubMedXML(xmlBytes)
		if err != nil {
			logger.L.Warn("pubmed xml parse failed", "err", err)
			continue
		}
		out = append(out, papers...)
		if len(out) >= limit {
			break
		}
	}

	// 截断到 limit；Published 没法严格保证顺序，后续 MultiSource 会再排序。
	if len(out) > limit {
		out = out[:limit]
	}
	for i := range out {
		out[i].Source = p.Name()
	}
	return out, nil
}

func httpGetBytes(ctx context.Context, c *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "onepaper/1.0")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(b), 300))
	}
	return b, nil
}

func parsePubMedXML(xmlBytes []byte) ([]model.Paper, error) {
	// PubMed EFETCH 返回的是 XML，结构固定但字段层级较深；这里只解析必要字段。
	type articleID struct {
		Type string `xml:"IdType,attr"`
		Val  string `xml:",chardata"`
	}
	type author struct {
		LastName       string `xml:"LastName"`
		ForeName       string `xml:"ForeName"`
		CollectiveName string `xml:"CollectiveName"`
	}

	type pubMedArticle struct {
		PMID       string      `xml:"MedlineCitation>PMID"`
		Title      string      `xml:"MedlineCitation>Article>ArticleTitle"`
		Abstract   string      `xml:"MedlineCitation>Article>Abstract>AbstractText"`
		Authors    []author    `xml:"MedlineCitation>Article>AuthorList>Author"`
		ArticleIDs []articleID `xml:"MedlineCitation>Article>ArticleIdList>ArticleId"`
		PubDate    pubMedDate  `xml:"MedlineCitation>Article>Journal>JournalIssue>PubDate"`
	}

	var doc struct {
		Articles []pubMedArticle `xml:"PubmedArticle"`
	}
	if err := xml.Unmarshal(xmlBytes, &doc); err != nil {
		return nil, err
	}

	out := make([]model.Paper, 0, len(doc.Articles))
	for _, a := range doc.Articles {
		pmid := strings.TrimSpace(a.PMID)
		title := strings.TrimSpace(a.Title)
		abstract := strings.TrimSpace(a.Abstract)
		if pmid == "" || title == "" {
			continue
		}

		authors := make([]string, 0, len(a.Authors))
		for _, au := range a.Authors {
			if strings.TrimSpace(au.CollectiveName) != "" {
				authors = append(authors, strings.TrimSpace(au.CollectiveName))
				continue
			}
			last := strings.TrimSpace(au.LastName)
			first := strings.TrimSpace(au.ForeName)
			if last == "" && first == "" {
				continue
			}
			if first == "" {
				authors = append(authors, last)
			} else if last == "" {
				authors = append(authors, first)
			} else {
				authors = append(authors, first+" "+last)
			}
		}

		doi := ""
		for _, id := range a.ArticleIDs {
			if strings.EqualFold(strings.TrimSpace(id.Type), "doi") {
				doi = strings.TrimSpace(id.Val)
				break
			}
		}

		published := parsePubMedDate(a.PubDate)

		// PubMed 页面 URL（PDF URL 交给 Unpaywall）
		paper := model.Paper{
			ID:        pmid,
			Title:     title,
			Abstract:  abstract,
			Authors:   authors,
			URL:       "https://pubmed.ncbi.nlm.nih.gov/" + pmid + "/",
			DOI:       doi,
			PDFURL:    "",
			Published: published,
		}
		out = append(out, paper)
	}

	return out, nil
}

func parsePubMedDate(d pubMedDate) time.Time {
	yearStr := strings.TrimSpace(d.Year)
	if yearStr == "" {
		return time.Time{}
	}
	year, err := strconv.Atoi(yearStr)
	if err != nil || year <= 0 {
		return time.Time{}
	}

	month := time.January
	if strings.TrimSpace(d.Month) != "" {
		if m, err := strconv.Atoi(d.Month); err == nil && m >= 1 && m <= 12 {
			month = time.Month(m)
		} else {
			switch strings.ToLower(strings.TrimSpace(d.Month)) {
			case "jan":
				month = time.January
			case "feb":
				month = time.February
			case "mar":
				month = time.March
			case "apr":
				month = time.April
			case "may":
				month = time.May
			case "jun":
				month = time.June
			case "jul":
				month = time.July
			case "aug":
				month = time.August
			case "sep", "sept":
				month = time.September
			case "oct":
				month = time.October
			case "nov":
				month = time.November
			case "dec":
				month = time.December
			}
		}
	}

	day := 1
	if strings.TrimSpace(d.Day) != "" {
		if dd, err := strconv.Atoi(d.Day); err == nil && dd >= 1 && dd <= 31 {
			day = dd
		}
	}

	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
