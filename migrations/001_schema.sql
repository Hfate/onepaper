-- MySQL 8+

CREATE DATABASE IF NOT EXISTS onepaper DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE onepaper;

CREATE TABLE IF NOT EXISTS papers (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  paper_id VARCHAR(64) NOT NULL,
  title TEXT NOT NULL,
  abstract MEDIUMTEXT,
  score DOUBLE NULL,
  source VARCHAR(32) NOT NULL DEFAULT 'arxiv',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_paper_source (paper_id, source),
  KEY idx_created (created_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS articles (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  title VARCHAR(512) NOT NULL,
  content MEDIUMTEXT NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'draft',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_status (status),
  KEY idx_created (created_at)
) ENGINE=InnoDB;
