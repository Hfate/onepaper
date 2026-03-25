package repository

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

// OpenMySQL 打开 MySQL 连接。
func OpenMySQL(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	return db, nil
}
