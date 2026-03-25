package publisher

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Hfate/onepaper/internal/model"
	"github.com/Hfate/onepaper/pkg/logger"
	"github.com/silenceper/wechat/v2"
	"github.com/silenceper/wechat/v2/cache"
	"github.com/silenceper/wechat/v2/officialaccount"
	offcfg "github.com/silenceper/wechat/v2/officialaccount/config"
	"github.com/silenceper/wechat/v2/officialaccount/draft"
	"github.com/silenceper/wechat/v2/officialaccount/material"
)

// Config 公众号发布配置。
type Config struct {
	AppID          string
	AppSecret      string
	Author         string
	PublishMode    string // draft | publish | none
	Token          string
	EncodingAESKey string
	// DefaultThumb 无章节配图时上传为封面缩略图；须为本地可读文件（如项目根目录 default.png）。
	DefaultThumb string
}

// WeChatPublisher 图文素材与发布。
type WeChatPublisher struct {
	cfg Config
	wc  *wechat.Wechat
}

// New 创建发布器。
func New(cfg Config) *WeChatPublisher {
	mem := cache.NewMemory()
	wc := wechat.NewWechat()
	wc.SetCache(mem)
	return &WeChatPublisher{cfg: cfg, wc: wc}
}

func (p *WeChatPublisher) oa() *officialaccount.OfficialAccount {
	cfg := &offcfg.Config{
		AppID:          p.cfg.AppID,
		AppSecret:      p.cfg.AppSecret,
		Token:          p.cfg.Token,
		EncodingAESKey: p.cfg.EncodingAESKey,
		Cache:          nil, // 使用 wc.SetCache 注入的共享 cache
	}
	return p.wc.GetOfficialAccount(cfg)
}

// Publish 上传图片、渲染 HTML、写入草稿或发布。
func (p *WeChatPublisher) Publish(ctx context.Context, article *model.Article, render func(model.Article) string) error {
	_ = ctx
	mode := strings.ToLower(strings.TrimSpace(p.cfg.PublishMode))
	if mode == "none" {
		logger.L.Info("wechat publish skipped", "mode", mode)
		return nil
	}
	if strings.TrimSpace(p.cfg.AppID) == "" || strings.TrimSpace(p.cfg.AppSecret) == "" {
		return fmt.Errorf("wechat app_id/app_secret not configured (set wechat.publish_mode to none to skip)")
	}

	mat := p.oa().GetMaterial()
	for i := range article.Sections {
		sec := &article.Sections[i]
		if sec.LocalImage == "" {
			continue
		}
		url, err := mat.ImageUpload(sec.LocalImage)
		if err != nil {
			return fmt.Errorf("wechat image upload %s: %w", sec.LocalImage, err)
		}
		sec.ImageURL = url
		logger.L.Info("wechat image uploaded", "section", sec.Heading, "url", url)
	}

	article.HTML = render(*article)

	var thumbID string
	for _, sec := range article.Sections {
		if sec.LocalImage != "" {
			id, _, err := mat.AddMaterial(material.MediaTypeThumb, sec.LocalImage)
			if err != nil {
				return fmt.Errorf("thumb material: %w", err)
			}
			thumbID = id
			break
		}
	}
	if thumbID == "" {
		path := strings.TrimSpace(p.cfg.DefaultThumb)
		if path == "" {
			return fmt.Errorf("no thumbnail: add at least one section image or set wechat.default_thumb")
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("wechat default_thumb %q: %w", path, err)
		}
		id, _, err := mat.AddMaterial(material.MediaTypeThumb, path)
		if err != nil {
			return fmt.Errorf("default thumb material: %w", err)
		}
		thumbID = id
		logger.L.Info("wechat default thumb used", "path", path)
	}

	digest := truncateRunes(stripHTML(article.Intro), 120)
	da := &draft.Article{
		Title:            article.Title,
		Author:           p.cfg.Author,
		Digest:           digest,
		Content:          article.HTML,
		ContentSourceURL: "",
		ThumbMediaID:     thumbID,
		ShowCoverPic:     1,
	}

	dr := p.oa().GetDraft()
	mediaID, err := dr.AddDraft([]*draft.Article{da})
	if err != nil {
		return fmt.Errorf("add draft: %w", err)
	}
	logger.L.Info("wechat draft created", "media_id", mediaID)

	if mode == "publish" {
		fp := p.oa().GetFreePublish()
		pubID, err := fp.Publish(mediaID)
		if err != nil {
			return fmt.Errorf("free publish: %w", err)
		}
		logger.L.Info("wechat publish submitted", "publish_id", pubID, "note", "poll freepublish/get for status")
	}
	return nil
}

func stripHTML(s string) string {
	s = strings.ReplaceAll(s, "<br>", " ")
	s = strings.ReplaceAll(s, "<br/>", " ")
	return strings.TrimSpace(s)
}

func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
