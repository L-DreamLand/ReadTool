package state

import (
	"database/sql"
	"strings"
)

// Progress 是某个条目的阅读位置（FR-10 / FR-12）。
// 一个条目只有一行，后写覆盖：换台设备接着读，读到的是最后一次写入的位置。
type Progress struct {
	Path      string  `json:"path"`
	ScrollPct float64 `json:"scrollPct"`
	AnchorID  string  `json:"anchorId,omitempty"`
	Device    string  `json:"device,omitempty"`
	UpdatedAt int64   `json:"updatedAt"`
}

// PutProgress 写入（或覆盖）一个条目的阅读位置。updatedAt 由这里决定，不吃客户端时钟。
func (s *Store) PutProgress(p Progress, now int64) error {
	_, err := s.db.Exec(`
INSERT INTO progress(path, scroll_pct, anchor_id, device, updated_at) VALUES(?, ?, ?, ?, ?)
ON CONFLICT(path) DO UPDATE SET
  scroll_pct = excluded.scroll_pct,
  anchor_id  = excluded.anchor_id,
  device     = excluded.device,
  updated_at = excluded.updated_at`,
		p.Path, p.ScrollPct, nullable(p.AnchorID), nullable(p.Device), now)
	return err
}

// Progress 读一个条目的阅读位置；没读过时 ok 为 false。
func (s *Store) Progress(path string) (Progress, bool, error) {
	row := s.db.QueryRow(`SELECT path, scroll_pct, anchor_id, device, updated_at
FROM progress WHERE path = ?`, path)
	return scanProgress(row)
}

// ProgressMany 批量读（首页一次性问多个条目"读到哪了"）。缺失的路径直接不返回，不报错。
func (s *Store) ProgressMany(paths []string) ([]Progress, error) {
	out := make([]Progress, 0, len(paths))
	if len(paths) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(paths))
	for _, p := range paths {
		args = append(args, p)
	}
	ph := strings.Repeat("?,", len(paths))
	rows, err := s.db.Query(`SELECT path, scroll_pct, anchor_id, device, updated_at
FROM progress WHERE path IN (`+ph[:len(ph)-1]+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, _, err := scanProgress(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RecentProgress 取最近读过的 limit 条，按写入时间倒序（FR-12 的「最近阅读」）。
func (s *Store) RecentProgress(limit int) ([]Progress, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.Query(`SELECT path, scroll_pct, anchor_id, device, updated_at
FROM progress ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Progress, 0, limit)
	for rows.Next() {
		p, _, err := scanProgress(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanProgress(row scannable) (Progress, bool, error) {
	var (
		p        Progress
		anchorID sql.NullString
		device   sql.NullString
	)
	err := row.Scan(&p.Path, &p.ScrollPct, &anchorID, &device, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return Progress{}, false, nil
	}
	if err != nil {
		return Progress{}, false, err
	}
	p.AnchorID = anchorID.String
	p.Device = device.String
	return p, true, nil
}

// nullable 把空串存成 NULL：SQLite 的 STRICT 表允许，但列表里更好区分"没填"与"填了空"。
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
