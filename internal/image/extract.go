package image

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Hfate/onepaper/internal/model"
	"github.com/Hfate/onepaper/pkg/logger"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfmodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// Extractor 论文配图提取（arXiv HTML 优先，PDF 回退）。
type Extractor struct {
	HTTP        *http.Client // HTML 页、小图
	PDFHTTP     *http.Client // 整本 PDF，单独更长超时
	PdfFallback bool         // false：仅 HTML 配图，失败即返回；true：再尝试整本 PDF
	Dir         string
	MinW        int
	MinH        int
}

var imgSrcRe = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)

// ExtractMainImage 返回本地保存路径；失败返回错误。
func (e *Extractor) httpClient() *http.Client {
	if e.HTTP != nil {
		return e.HTTP
	}
	return &http.Client{Timeout: 90 * time.Second}
}

func (e *Extractor) pdfClient() *http.Client {
	if e.PDFHTTP != nil {
		return e.PDFHTTP
	}
	return e.httpClient()
}

func (e *Extractor) ExtractMainImage(ctx context.Context, p model.Paper) (string, error) {
	if e.MinW <= 0 {
		e.MinW = 500
	}
	if e.MinH <= 0 {
		e.MinH = 500
	}
	if err := os.MkdirAll(e.Dir, 0o755); err != nil {
		return "", err
	}

	// 1) arXiv 摘要页 / HTML 实验页
	if strings.Contains(strings.ToLower(p.URL), "arxiv.org") {
		logger.L.Info("image: try arxiv html", "id", p.ID)
		if path, err := e.fromArxivHTML(ctx, p); err == nil && path != "" {
			return path, nil
		} else if err != nil {
			logger.L.Warn("arxiv html image failed", "id", p.ID, "err", err)
		}
	}

	if !e.PdfFallback {
		return "", fmt.Errorf("pdf fallback disabled")
	}

	// 2) PDF 第一页大图（整本下载，可能较慢）
	logger.L.Info("image: try pdf extract page 1", "id", p.ID, "pdf", p.PDFURL)
	path, err := e.fromPDF(ctx, p)
	if err != nil {
		return "", err
	}
	return path, nil
}

func (e *Extractor) fromArxivHTML(ctx context.Context, p model.Paper) (string, error) {
	urls := []string{
		"https://arxiv.org/html/" + p.ID,
		p.URL,
	}
	seen := map[string]struct{}{}
	for _, pageURL := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "onepaper-bot/1.0")
		resp, err := e.httpClient().Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		html := string(body)
		matches := imgSrcRe.FindAllStringSubmatch(html, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			src := strings.TrimSpace(m[1])
			abs := resolveURL(pageURL, src)
			if _, ok := seen[abs]; ok {
				continue
			}
			seen[abs] = struct{}{}
			if len(seen) > 25 {
				break
			}
			path, err := e.downloadIfLargeEnough(ctx, abs, p.ID+"_fig")
			if err == nil && path != "" {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("no suitable image in arxiv html")
}

func resolveURL(baseStr, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	if strings.HasPrefix(ref, "//") {
		return "https:" + ref
	}
	bu, err := url.Parse(baseStr)
	if err != nil {
		return ref
	}
	ru, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return bu.ResolveReference(ru).String()
}

func (e *Extractor) downloadIfLargeEnough(ctx context.Context, imgURL, filePrefix string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "onepaper-bot/1.0")
	resp, err := e.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	if cfg.Width < e.MinW || cfg.Height < e.MinH {
		return "", fmt.Errorf("image too small %dx%d", cfg.Width, cfg.Height)
	}
	ext := ".png"
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "jpeg") || strings.Contains(ct, "jpg") {
		ext = ".jpg"
	} else if strings.Contains(ct, "gif") {
		ext = ".gif"
	}
	path := filepath.Join(e.Dir, sanitize(filePrefix)+ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	logger.L.Info("saved image", "path", path, "w", cfg.Width, "h", cfg.Height)
	return path, nil
}

func (e *Extractor) fromPDF(ctx context.Context, p model.Paper) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.PDFURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "onepaper-bot/1.0")
	resp, err := e.pdfClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("pdf download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("pdf status %d", resp.StatusCode)
	}
	pdfBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	rs := bytes.NewReader(pdfBytes)
	raw, err := api.ExtractImagesRaw(rs, []string{"1"}, nil)
	if err != nil {
		return "", fmt.Errorf("pdf extract: %w", err)
	}
	var best pdfmodel.Image
	found := false
	for _, pageMap := range raw {
		for _, img := range pageMap {
			if img.IsImgMask || img.Thumb {
				continue
			}
			if img.Width >= e.MinW && img.Height >= e.MinH {
				if !found || img.Width*img.Height > best.Width*best.Height {
					best = img
					found = true
				}
			}
		}
	}
	if !found {
		return "", fmt.Errorf("no large image on pdf page 1")
	}
	data, err := io.ReadAll(&best)
	if err != nil {
		return "", err
	}
	_, format, decErr := image.DecodeConfig(bytes.NewReader(data))
	ext := ".bin"
	if decErr == nil && format != "" {
		ext = "." + format
	}
	path := filepath.Join(e.Dir, sanitize(p.ID)+"_pdf"+ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	logger.L.Info("saved pdf image", "path", path)
	return path, nil
}

func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		default:
			return r
		}
	}, s)
	if s == "" {
		return "img"
	}
	return s
}
