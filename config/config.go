package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 应用配置（可从 YAML 加载，敏感字段支持 ${ENV_VAR}）。
type Config struct {
	Server struct {
		Addr string `yaml:"addr"`
	} `yaml:"server"`

	Database struct {
		DSN string `yaml:"dsn"`
	} `yaml:"database"`

	AI struct {
		BaseURL        string `yaml:"base_url"`
		APIKey         string `yaml:"api_key"`
		Model          string `yaml:"model"`
		ScoreModel     string `yaml:"score_model"`
		ArticleModel   string `yaml:"article_model"`
		MaxRetries     int    `yaml:"max_retries"`
		RequestTimeout string `yaml:"request_timeout"` // 单次 /chat/completions 超时（长文生成可 180s–300s）
	} `yaml:"ai"`

	Crawler struct {
		ArxivMaxResults int `yaml:"arxiv_max_results"`
		LookbackHours   int `yaml:"lookback_hours"`
	} `yaml:"crawler"`

	Filter struct {
		TopN int `yaml:"top_n"`
	} `yaml:"filter"`

	Summarizer struct {
		MinWords int `yaml:"min_words"`
		MaxWords int `yaml:"max_words"`
	} `yaml:"summarizer"`

	Image struct {
		Dir                string `yaml:"dir"`
		MinWidth           int    `yaml:"min_width"`
		MinHeight          int    `yaml:"min_height"`
		DownloadTimeout    string `yaml:"download_timeout"`     // HTML/小图
		PdfDownloadTimeout string `yaml:"pdf_download_timeout"` // 整本 PDF 下载
		PdfFallback        bool   `yaml:"pdf_fallback"`         // true 时 HTML 无图再下 PDF；默认 false 快速失败
	} `yaml:"image"`

	WeChat struct {
		AppID          string `yaml:"app_id"`
		AppSecret      string `yaml:"app_secret"`
		Author         string `yaml:"author"`
		PublishMode    string `yaml:"publish_mode"`     // draft | publish | none
		Token          string `yaml:"token"`            // 公众平台「服务器配置」Token
		EncodingAESKey string `yaml:"encoding_aes_key"` // 明文/兼容/安全模式下的 EncodingAESKey
		PushPath       string `yaml:"push_path"`        // 本服务回调路径，如 /api/v1/wechat/serve
		DefaultThumb   string `yaml:"default_thumb"`    // 无章节配图时用作封面；默认 default.png
	} `yaml:"wechat"`

	Scheduler struct {
		Cron     string `yaml:"cron"`
		Timezone string `yaml:"timezone"`
		Enabled  bool   `yaml:"enabled"`
	} `yaml:"scheduler"`

	OSS struct {
		Enabled   bool   `yaml:"enabled"`
		Endpoint  string `yaml:"endpoint"`
		Bucket    string `yaml:"bucket"`
		PublicURL string `yaml:"public_url"`
	} `yaml:"oss"`
}

// Load 从文件加载配置并展开环境变量占位符。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	expanded := os.ExpandEnv(string(data))
	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	cfg.AI.BaseURL = strings.TrimSpace(cfg.AI.BaseURL)
	if cfg.AI.BaseURL == "" {
		cfg.AI.BaseURL = strings.TrimSpace(os.Getenv("AI_BASE_URL"))
	}
	cfg.AI.APIKey = strings.TrimSpace(cfg.AI.APIKey)
	if cfg.AI.APIKey == "" {
		cfg.AI.APIKey = resolveAIAPIKeyFromEnv()
	}
	applyDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// resolveAIAPIKeyFromEnv 在 yaml 未提供 api_key 时，按优先级读取环境变量（OpenAI 兼容网关通用）。
func resolveAIAPIKeyFromEnv() string {
	keys := []string{
		"AI_API_KEY",
		"LLM_API_KEY",
		"OPENROUTER_API_KEY",
		"DEEPSEEK_API_KEY",
		"OPENAI_API_KEY",
	}
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.AI.Model == "" {
		cfg.AI.Model = "gpt-4o-mini"
	}
	if cfg.AI.ScoreModel == "" {
		cfg.AI.ScoreModel = cfg.AI.Model
	}
	if cfg.AI.ArticleModel == "" {
		cfg.AI.ArticleModel = cfg.AI.Model
	}
	if cfg.AI.MaxRetries < 0 {
		cfg.AI.MaxRetries = 0
	}
	if cfg.AI.RequestTimeout == "" {
		// 长文生成（article）输出较慢时，45s 偏紧；提升默认值以避免 read body 超时。
		cfg.AI.RequestTimeout = "180s"
	}
	if cfg.Crawler.ArxivMaxResults <= 0 {
		cfg.Crawler.ArxivMaxResults = 20
	}
	if cfg.Crawler.LookbackHours <= 0 {
		cfg.Crawler.LookbackHours = 24
	}
	if cfg.Filter.TopN <= 0 {
		cfg.Filter.TopN = 5
	}
	if cfg.Summarizer.MinWords <= 0 {
		cfg.Summarizer.MinWords = 1500
	}
	if cfg.Summarizer.MaxWords <= 0 {
		cfg.Summarizer.MaxWords = 2500
	}
	if cfg.Image.Dir == "" {
		cfg.Image.Dir = "./data/images"
	}
	if cfg.Image.MinWidth <= 0 {
		cfg.Image.MinWidth = 500
	}
	if cfg.Image.MinHeight <= 0 {
		cfg.Image.MinHeight = 500
	}
	if cfg.Image.DownloadTimeout == "" {
		cfg.Image.DownloadTimeout = "90s"
	}
	if cfg.Image.PdfDownloadTimeout == "" {
		cfg.Image.PdfDownloadTimeout = "60s"
	}
	if cfg.WeChat.PublishMode == "" {
		cfg.WeChat.PublishMode = "draft"
	}
	if cfg.Scheduler.Cron == "" {
		cfg.Scheduler.Cron = "0 9 * * *"
	}
	if cfg.Scheduler.Timezone == "" {
		cfg.Scheduler.Timezone = "Asia/Shanghai"
	}
	if cfg.WeChat.PushPath == "" {
		cfg.WeChat.PushPath = "/api/v1/wechat/serve"
	}
	if cfg.WeChat.DefaultThumb == "" {
		cfg.WeChat.DefaultThumb = "default.png"
	}
}

func validate(cfg *Config) error {
	// database.dsn 可选：为空时仅跑流水线不落库
	if strings.TrimSpace(cfg.AI.BaseURL) == "" {
		return fmt.Errorf("ai.base_url is required")
	}
	if strings.TrimSpace(cfg.AI.APIKey) == "" {
		return fmt.Errorf("ai.api_key is required (yaml/AI_API_KEY/OPENROUTER_API_KEY/DEEPSEEK_API_KEY/OPENAI_API_KEY 等)")
	}
	mode := strings.ToLower(cfg.WeChat.PublishMode)
	if mode != "draft" && mode != "publish" && mode != "none" {
		return fmt.Errorf("wechat.publish_mode must be draft, publish, or none")
	}
	return nil
}

// ImageDownloadTimeout 解析 HTML/配图小图下载超时。
func (c *Config) ImageDownloadTimeout() time.Duration {
	d, err := time.ParseDuration(c.Image.DownloadTimeout)
	if err != nil {
		return 90 * time.Second
	}
	return d
}

// ImagePDFDownloadTimeout 解析 arXiv PDF 整本下载超时（体积大、易超时）。
func (c *Config) ImagePDFDownloadTimeout() time.Duration {
	d, err := time.ParseDuration(c.Image.PdfDownloadTimeout)
	if err != nil {
		return 60 * time.Second
	}
	return d
}

// AIRequestTimeout 解析 LLM HTTP 单次请求超时。
func (c *Config) AIRequestTimeout() time.Duration {
	d, err := time.ParseDuration(c.AI.RequestTimeout)
	if err != nil {
		return 45 * time.Second
	}
	return d
}
