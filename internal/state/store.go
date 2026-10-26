// Package state 是唯一允许写盘的地方：只放阅读状态与密钥，绝不动知识库。
package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // 纯 Go 驱动：CGO_ENABLED=0 也能交叉编译
)

// Store 是状态库句柄。
type Store struct {
	db   *sql.DB
	path string
}

// Open 打开（必要时创建）状态库。
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("无法创建状态目录：%w", err)
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // 单连接：避免 SQLITE_BUSY，写操作本来就很少
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("状态库不可用：%w", err)
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Path 返回状态库文件路径。
func (s *Store) Path() string { return s.path }

// Close 关闭状态库。
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	// 表结构见 软件需求设计文档 §8.3。用 IF NOT EXISTS 而不是版本号迁移：
	// 目前只有"新建表"这一种变更，加列时再引入 user_version。
	const ddl = `
CREATE TABLE IF NOT EXISTS secrets (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;

-- 阅读进度：每条目一行，后写覆盖（FR-10）
CREATE TABLE IF NOT EXISTS progress (
  path        TEXT PRIMARY KEY,
  scroll_pct  REAL NOT NULL,
  anchor_id   TEXT,
  device      TEXT,
  updated_at  INTEGER NOT NULL
) STRICT;

-- 书签：条目级（anchor_id 为空）或锚点级（FR-11）
CREATE TABLE IF NOT EXISTS bookmarks (
  id          TEXT PRIMARY KEY,
  path        TEXT NOT NULL,
  anchor_id   TEXT,
  anchor_text TEXT,
  device      TEXT,
  created_at  INTEGER NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS idx_bookmarks_path ON bookmarks(path);

-- 最近阅读要按时间倒序扫，走这个索引省一次全表排序
CREATE INDEX IF NOT EXISTS idx_progress_updated ON progress(updated_at DESC);`
	if _, err := s.db.Exec(ddl); err != nil {
		return fmt.Errorf("初始化状态库失败：%w", err)
	}
	return nil
}

// Secret 读一个密钥；不存在时 ok 为 false。
func (s *Store) Secret(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM secrets WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// PutSecret 写一个密钥（覆盖）。
func (s *Store) PutSecret(key, value string) error {
	_, err := s.db.Exec(`
INSERT INTO secrets(key, value, updated_at) VALUES(?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().UnixMilli())
	return err
}

// DeleteSecret 删除密钥（轮换会话密钥时用）。
func (s *Store) DeleteSecret(key string) error {
	_, err := s.db.Exec(`DELETE FROM secrets WHERE key = ?`, key)
	return err
}
