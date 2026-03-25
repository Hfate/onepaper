package summarizer

import (
	"fmt"
	"strings"

	"github.com/Hfate/onepaper/internal/model"
)

const (
	baseStyle  = "font-size:16px;line-height:1.8;color:#333333;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;"
	h1Style    = "text-align:center;font-size:22px;font-weight:bold;margin:24px 0 16px;color:#111111;"
	h2Style    = "font-size:18px;font-weight:bold;margin:28px 0 12px;color:#1a1a1a;border-left:4px solid #07c160;padding-left:10px;"
	pStyle     = "margin:12px 0;text-align:justify;"
	imgWrap    = "text-align:center;margin:20px 0;"
	imgStyle   = "max-width:100%;height:auto;display:block;margin:0 auto;border-radius:4px;"
	summaryBox = "margin-top:28px;padding:16px;background:#f7f7f7;border-radius:8px;"
)

// RenderHTML 输出微信公众号友好的 inline CSS HTML。
func RenderHTML(article model.Article) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<section style="%s">`, baseStyle))
	b.WriteString(fmt.Sprintf(`<h1 style="%s">%s</h1>`, h1Style, escape(article.Title)))
	b.WriteString(fmt.Sprintf(`<p style="%s">%s</p>`, pStyle, escape(article.Intro)))

	for _, sec := range article.Sections {
		b.WriteString(fmt.Sprintf(`<h2 style="%s">%s</h2>`, h2Style, escape(sec.Heading)))
		if sec.ImageURL != "" {
			b.WriteString(fmt.Sprintf(`<section style="%s"><img src="%s" style="%s" alt="%s"/></section>`,
				imgWrap, escapeAttr(sec.ImageURL), imgStyle, escape(sec.Heading)))
		}
		for _, para := range splitParagraphs(sec.Body) {
			b.WriteString(fmt.Sprintf(`<p style="%s">%s</p>`, pStyle, escape(para)))
		}
	}

	b.WriteString(fmt.Sprintf(`<section style="%s">`, summaryBox))
	b.WriteString(fmt.Sprintf(`<p style="%s"><strong>小结</strong></p>`, pStyle))
	for _, para := range splitParagraphs(article.Summary) {
		b.WriteString(fmt.Sprintf(`<p style="%s">%s</p>`, pStyle, escape(para)))
	}
	b.WriteString(`</section>`)
	b.WriteString(`</section>`)
	return b.String()
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
