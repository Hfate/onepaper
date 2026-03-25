package repository

import (
	"context"
	"database/sql"
)

// InsertArticle 插入文章记录。
func InsertArticle(ctx context.Context, db *sql.DB, title, content, status string) (int64, error) {
	res, err := db.ExecContext(ctx, `
INSERT INTO articles (title, content, status, created_at)
VALUES (?, ?, ?, NOW())
`, title, content, status)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateArticleStatus 更新状态。
func UpdateArticleStatus(ctx context.Context, db *sql.DB, id int64, status string) error {
	_, err := db.ExecContext(ctx, `UPDATE articles SET status=? WHERE id=?`, status, id)
	return err
}
