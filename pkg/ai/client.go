package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Hfate/onepaper/pkg/logger"
	"golang.org/x/time/rate"
)

// Client OpenAI 兼容 Chat Completions 客户端（带简单重试与限速）。
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	maxRetries int
	limiter    *rate.Limiter
}

// New 创建客户端。baseURL 例如 https://api.openai.com/v1（无尾部斜杠）。
func New(baseURL, apiKey string, maxRetries int) *Client {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		maxRetries: maxRetries,
		limiter:    rate.NewLimiter(rate.Limit(20), 5),
	}
}

// ChatRequest 最小请求体。
type ChatRequest struct {
	Model          string    `json:"model"`
	Messages       []Message `json:"messages"`
	Temperature    float64   `json:"temperature,omitempty"`
	MaxTokens      int       `json:"max_tokens,omitempty"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format,omitempty"`
}

// Message 单条消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatResponse 解析响应。
type ChatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ChatCompletion 调用 /v1/chat/completions，带重试。
func (c *Client) ChatCompletion(ctx context.Context, model string, req ChatRequest) (string, error) {
	req.Model = model
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return "", err
		}
		body, err := json.Marshal(req)
		if err != nil {
			return "", err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		// OpenRouter 可选统计（文档称 HTTP-Referer，标准头为 Referer）
		if ref := strings.TrimSpace(os.Getenv("OPENROUTER_HTTP_REFERER")); ref != "" {
			httpReq.Header.Set("Referer", ref)
		}
		if title := strings.TrimSpace(os.Getenv("OPENROUTER_X_TITLE")); title != "" {
			httpReq.Header.Set("X-Title", title)
		}

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			logger.L.Warn("ai request failed", "attempt", attempt, "err", err)
			time.Sleep(backoff(attempt))
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var out ChatResponse
		if err := json.Unmarshal(b, &out); err != nil {
			lastErr = err
			logger.L.Warn("ai decode failed", "attempt", attempt, "body", truncate(string(b), 500))
			time.Sleep(backoff(attempt))
			continue
		}
		if out.Error != nil {
			lastErr = fmt.Errorf("%s", out.Error.Message)
			if retryableStatus(resp.StatusCode) || isRateLimit(out.Error.Message) {
				logger.L.Warn("ai api error", "attempt", attempt, "msg", out.Error.Message)
				time.Sleep(backoff(attempt))
				continue
			}
			return "", lastErr
		}
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(b), 500))
			if retryableStatus(resp.StatusCode) {
				time.Sleep(backoff(attempt))
				continue
			}
			return "", lastErr
		}
		if len(out.Choices) == 0 {
			lastErr = errors.New("empty choices")
			time.Sleep(backoff(attempt))
			continue
		}
		return strings.TrimSpace(out.Choices[0].Message.Content), nil
	}
	if lastErr == nil {
		lastErr = errors.New("exhausted retries")
	}
	return "", lastErr
}

func backoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 200 * time.Millisecond
	}
	d := time.Duration(1<<uint(attempt)) * 200 * time.Millisecond
	if d > 10*time.Second {
		return 10 * time.Second
	}
	return d
}

func retryableStatus(code int) bool {
	return code == 429 || code >= 500
}

func isRateLimit(msg string) bool {
	return strings.Contains(strings.ToLower(msg), "rate")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
