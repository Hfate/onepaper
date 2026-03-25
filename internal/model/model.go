package model

import "time"

// Paper 论文元数据（跨抓取源统一结构）。
type Paper struct {
	ID        string
	Title     string
	Abstract  string
	Authors   []string
	URL       string
	PDFURL    string
	Published time.Time
	Source    string
}

// ScoreResult AI 评分结果。
type ScoreResult struct {
	Novelty        float64 `json:"novelty"`
	Impact         float64 `json:"impact"`
	PublicInterest float64 `json:"public_interest"`
	Score          float64 `json:"score"`
	Reason         string  `json:"reason"`
}

// PaperScore 论文与分数绑定。
type PaperScore struct {
	Paper Paper
	Score ScoreResult
}

// ArticleSection 公众号文章中的一节（对应一篇论文）。
type ArticleSection struct {
	PaperID    string
	Heading    string
	Body       string
	LocalImage string // 本地路径，发布前填充
	ImageURL   string // 微信图床 URL，上传后填充
}

// Article 生成的中文科普稿。
type Article struct {
	Title    string
	Intro    string
	Sections []ArticleSection
	Summary  string
	HTML     string // RenderHTML 输出
}

// ArticleRecord DB 中的文章记录。
type ArticleRecord struct {
	ID        int64
	Title     string
	Content   string
	Status    string
	CreatedAt time.Time
}

// PaperRecord DB 中的论文记录。
type PaperRecord struct {
	ID        int64
	PaperID   string
	Title     string
	Abstract  string
	Score     *float64
	Source    string
	CreatedAt time.Time
}
