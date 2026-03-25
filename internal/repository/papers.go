package repository

import (
	"context"
	"database/sql"

	"github.com/Hfate/onepaper/internal/model"
)

// SavePaper 插入或更新论文分数（按 paper_id + source 唯一语义由业务保证）。
func SavePaper(ctx context.Context, db *sql.DB, p model.Paper, score *float64, source string) error {
	_, err := db.ExecContext(ctx, `
INSERT INTO papers (paper_id, title, abstract, score, source, created_at)
VALUES (?, ?, ?, ?, ?, NOW())
ON DUPLICATE KEY UPDATE title=VALUES(title), abstract=VALUES(abstract), score=VALUES(score), source=VALUES(source)
`, p.ID, p.Title, p.Abstract, score, source)
	return err
}
