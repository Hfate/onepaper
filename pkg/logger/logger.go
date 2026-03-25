package logger

import (
	"log/slog"
	"os"
)

// L 全局 slog，各模块统一使用。
var L *slog.Logger

func init() {
	L = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

// Set 替换默认 logger（例如测试时）。
func Set(l *slog.Logger) {
	if l != nil {
		L = l
	}
}
