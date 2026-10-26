package state

import (
	"database/sql"
)

// Bookmark 指向「某条目 + 可选锚点」，绝不修改知识库原文（FR-11）。
type Bookmark struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	AnchorID   string `json:"anchorId,omitempty"`
	AnchorText string `json:"anchorText,omitempty"`
	Device     string `json:"device,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
}

// AddBookmark 插入一条书签。id 由调用方生成，保证同一请求幂等判断简单。
func (s *Store) AddBookmark(b Bookmark) error {
	_, err := s.db.Exec(`INSERT INTO bookmarks(id, path, anchor_id, anchor_text, device, created_at)
VALUES(?, ?, ?, ?, ?, ?)`,
		b.ID, b.Path, nullable(b.AnchorID), nullable(b.AnchorText), nullable(b.Device), b.CreatedAt)
	return err
}

// ListBookmarks 按创建时间倒序返回全部书签（上限由写入侧控制）。
func (s *Store) ListBookmarks() ([]Bookmark, error) {
	rows, err := s.db.Query(`SELECT id, path, anchor_id, anchor_text, device, created_at
FROM bookmarks ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Bookmark, 0, 16)
	for rows.Next() {
		var (
			b          Bookmark
			anchorID   sql.NullString
			anchorText sql.NullString
			device     sql.NullString
		)
		if err := rows.Scan(&b.ID, &b.Path, &anchorID, &anchorText, &device, &b.CreatedAt); err != nil {
			return nil, err
		}
		b.AnchorID = anchorID.String
		b.AnchorText = anchorText.String
		b.Device = device.String
		out = append(out, b)
	}
	return out, rows.Err()
}

// DeleteBookmark 删除一条书签；不存在时 found 为 false。
func (s *Store) DeleteBookmark(id string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM bookmarks WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CountBookmarks 用于 500 条上限判断（设计规格 D-2）。
func (s *Store) CountBookmarks() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM bookmarks`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
