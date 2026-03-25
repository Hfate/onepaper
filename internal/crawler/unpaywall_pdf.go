package crawler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Hfate/onepaper/internal/model"
	"github.com/Hfate/onepaper/pkg/logger"
)

// UnpaywallPDFResolver 使用 Unpaywall 为 DOI 查找开放获取的 PDF URL。
// 查找失败或无法获得 PDFURL 的论文会被丢弃（fail_fast 由外层控制）。
type UnpaywallPDFResolver struct {
	Email   string
	BaseURL string
	HTTP    *http.Client
}

type unpaywallResp struct {
	BestOALocation *struct {
		URLForPDF string `json:"url_for_pdf"`
		URL       string `json:"url"`
	} `json:"best_oa_location"`

	Locations []struct {
		URLForPDF string `json:"url_for_pdf"`
		URL       string `json:"url"`
	} `json:"oa_locations"`
}

func (r *UnpaywallPDFResolver) httpClient() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Resolve 返回“已补齐 PDFURL/或原本就有 PDFURL”的论文集合。
func (r *UnpaywallPDFResolver) Resolve(ctx context.Context, papers []model.Paper) ([]model.Paper, error) {
	if strings.TrimSpace(r.Email) == "" {
		// Email 为空时，直接跳过，不改变当前来源行为（由外层的 require_pdf 决定最终是否失败）。
		logger.L.Info("unpaywall disabled: empty email")
		return papers, nil
	}
	baseURL := strings.TrimSpace(r.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.unpaywall.org/v2/"
	}

	out := make([]model.Paper, 0, len(papers))
	client := r.httpClient()

	for _, p := range papers {
		if strings.TrimSpace(p.PDFURL) != "" {
			out = append(out, p)
			continue
		}
		doi := strings.TrimSpace(p.DOI)
		if doi == "" {
			continue
		}

		base := strings.TrimRight(baseURL, "/") + "/"
		u := base + url.PathEscape(doi) + "?email=" + url.QueryEscape(r.Email)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "onepaper/1.0")
		resp, err := client.Do(req)
		if err != nil {
			logger.L.Warn("unpaywall request failed", "paper", p.ID, "doi", doi, "err", err)
			continue
		}
		var b []byte
		b, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			logger.L.Warn("unpaywall read failed", "paper", p.ID, "doi", doi, "err", err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			logger.L.Warn("unpaywall bad status", "paper", p.ID, "doi", doi, "status", resp.StatusCode, "body", truncate(string(b), 200))
			continue
		}

		var ur unpaywallResp
		if err := json.Unmarshal(b, &ur); err != nil {
			logger.L.Warn("unpaywall json parse failed", "paper", p.ID, "doi", doi, "err", err)
			continue
		}

		pdfURL := extractPDFURL(&ur)
		if strings.TrimSpace(pdfURL) == "" {
			continue
		}
		p.PDFURL = pdfURL
		out = append(out, p)
	}
	return out, nil
}

func extractPDFURL(r *unpaywallResp) string {
	if r == nil {
		return ""
	}
	if r.BestOALocation != nil {
		if strings.TrimSpace(r.BestOALocation.URLForPDF) != "" {
			return r.BestOALocation.URLForPDF
		}
		if strings.TrimSpace(r.BestOALocation.URL) != "" {
			return r.BestOALocation.URL
		}
	}
	for _, loc := range r.Locations {
		if strings.TrimSpace(loc.URLForPDF) != "" {
			return loc.URLForPDF
		}
		if strings.TrimSpace(loc.URL) != "" {
			return loc.URL
		}
	}
	return ""
}
