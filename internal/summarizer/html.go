package summarizer

import (
	"fmt"
	"strings"

	"github.com/Hfate/onepaper/internal/model"
)

const (
	baseStyle = "font-size:16px;line-height:1.85;color:#2c2c2c;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,'PingFang SC','Hiragino Sans GB','Microsoft YaHei',sans-serif;"
	h1Style   = "text-align:center;font-size:23px;font-weight:700;margin:8px 0 20px;color:#111111;letter-spacing:0.02em;"
	// 核心观点：与 h2 左边框区分，用浅底 + 左侧强调色
	coreBoxStyle = "margin:20px 0 24px;padding:16px 18px;background:linear-gradient(135deg,#f0f9f4 0%,#f7f7f7 100%);border-radius:10px;border-left:5px solid #07c160;box-shadow:0 1px 3px rgba(0,0,0,0.06);"
	coreLabel    = "font-size:13px;font-weight:600;color:#059669;margin:0 0 8px;letter-spacing:0.08em;"
	coreText     = "font-size:16px;line-height:1.8;margin:0;color:#1f2937;"
	pStyle       = "margin:14px 0;text-align:justify;text-indent:0;"
	h2Style      = "font-size:19px;font-weight:700;margin:32px 0 14px;color:#111827;border-left:4px solid #2563eb;padding-left:12px;line-height:1.35;"
	imgWrap      = "text-align:center;margin:18px 0 8px;"
	imgStyle     = "max-width:100%;height:auto;display:block;margin:0 auto;border-radius:6px;box-shadow:0 2px 8px rgba(0,0,0,0.08);"
	// 简要评论：引用样式
	commentStyle = "margin:12px 0 16px;padding:12px 16px;background:#fafafa;border-left:3px solid #94a3b8;border-radius:0 8px 8px 0;font-size:15px;line-height:1.75;color:#475569;font-style:italic;"
	// 元数据行
	metaStyle  = "margin:18px 0 8px;padding-top:12px;border-top:1px solid #e5e7eb;font-size:14px;line-height:1.7;color:#6b7280;"
	linkStyle  = "color:#2563eb;text-decoration:none;border-bottom:1px solid rgba(37,99,235,0.35);"
	summaryBox = "margin-top:36px;padding:18px 20px;background:#f3f4f6;border-radius:10px;border:1px solid #e5e7eb;"
	summaryLbl = "font-size:15px;font-weight:600;color:#374151;margin:0 0 10px;"
)

// RenderHTML 输出微信公众号友好的 inline CSS HTML（固定结构：核心观点 → 导语 → 分节配图/评论/正文/元数据 → 小结）。
func RenderHTML(article model.Article) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<section style="%s">`, baseStyle))
	b.WriteString(fmt.Sprintf(`<h1 style="%s">%s</h1>`, h1Style, escape(article.Title)))

	if strings.TrimSpace(article.CoreViewpoint) != "" {
		b.WriteString(fmt.Sprintf(`<section style="%s">`, coreBoxStyle))
		b.WriteString(fmt.Sprintf(`<p style="%s">核心观点</p>`, coreLabel))
		for _, para := range splitParagraphs(article.CoreViewpoint) {
			b.WriteString(fmt.Sprintf(`<p style="%s">%s</p>`, coreText, escape(para)))
		}
		b.WriteString(`</section>`)
	}

	if strings.TrimSpace(article.Intro) != "" {
		for _, para := range splitParagraphs(article.Intro) {
			b.WriteString(fmt.Sprintf(`<p style="%s">%s</p>`, pStyle, escape(para)))
		}
	}

	for _, sec := range article.Sections {
		b.WriteString(fmt.Sprintf(`<h2 style="%s">%s</h2>`, h2Style, escape(sec.Heading)))
		imgSrc := strings.TrimSpace(sec.ImageURL)
		if imgSrc == "" {
			imgSrc = strings.TrimSpace(sec.LocalImage)
		}
		if imgSrc != "" {
			b.WriteString(fmt.Sprintf(`<section style="%s"><img src="%s" style="%s" alt="%s"/></section>`,
				imgWrap, escapeAttr(imgSrc), imgStyle, escape(sec.Heading)))
		}
		if strings.TrimSpace(sec.ShortComment) != "" {
			b.WriteString(fmt.Sprintf(`<blockquote style="%s">%s</blockquote>`, commentStyle, escape(sec.ShortComment)))
		}
		for _, para := range splitParagraphs(sec.Body) {
			b.WriteString(fmt.Sprintf(`<p style="%s">%s</p>`, pStyle, escape(para)))
		}
		meta := formatSectionMeta(sec.AuthorsLine, sec.SourceURL)
		if meta != "" {
			b.WriteString(fmt.Sprintf(`<p style="%s">%s</p>`, metaStyle, meta))
		}
	}

	if strings.TrimSpace(article.Summary) != "" {
		b.WriteString(fmt.Sprintf(`<section style="%s">`, summaryBox))
		b.WriteString(fmt.Sprintf(`<p style="%s">小结</p>`, summaryLbl))
		for _, para := range splitParagraphs(article.Summary) {
			b.WriteString(fmt.Sprintf(`<p style="%s">%s</p>`, pStyle, escape(para)))
		}
		b.WriteString(`</section>`)
	}

	b.WriteString(`</section>`)
	return b.String()
}

func formatSectionMeta(authorsLine, sourceURL string) string {
	authorsLine = strings.TrimSpace(authorsLine)
	sourceURL = strings.TrimSpace(sourceURL)
	var parts []string
	if authorsLine != "" {
		parts = append(parts, fmt.Sprintf(`作者：%s`, escape(authorsLine)))
	}
	if sourceURL != "" {
		parts = append(parts, fmt.Sprintf(`<a href="%s" style="%s" target="_blank" rel="noopener noreferrer">阅读原文</a>`,
			escapeAttr(sourceURL), linkStyle))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "　")
}

func splitParagraphs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{s}
	}
	return out
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func escapeAttr(s string) string {
	return escape(s)
}
