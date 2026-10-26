// Package kb 负责把磁盘上的知识库变成可查询的目录树。
// 它不渲染、不打 HTTP，只回答"有哪些条目、某个相对路径指向哪里"。
package kb

import "time"

// Entry 是一个知识条目（一篇 .md）。
type Entry struct {
	Path      string   `json:"path"`     // 相对 Root，统一 "/"
	Category  string   `json:"category"` // 一级分类；根目录文件为 "根目录"
	Name      string   `json:"name"`     // 显示名
	Summary   string   `json:"summary,omitempty"`
	Keywords  []string `json:"keywords,omitempty"`
	Updated   string   `json:"updated,omitempty"`
	Size      int64    `json:"size"`
	Unindexed bool     `json:"unindexed"` // index.json 里没有登记

	ModTime time.Time `json:"-"`
	order   int       // 分类内稳定顺序
}

// Category 是一个一级分类。
type Category struct {
	Name    string  `json:"name"`
	Desc    string  `json:"desc"`
	Count   int     `json:"count"`
	Entries []Entry `json:"entries"`
}

// RootFile 是放在知识库根目录的文件（INDEX.md / AGENTS.md / 其他）。
type RootFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Kind string `json:"kind"` // index | agents | other
}

// Catalog 是一次扫描的完整结果。
type Catalog struct {
	KBRoot     string     `json:"kbRoot"`
	Version    int64      `json:"version"`
	Total      int        `json:"total"`
	Categories []Category `json:"categories"`
	RootFiles  []RootFile `json:"rootFiles"`

	byPath map[string]*Entry
	seq    []string // 按 (分类顺序, 分类内顺序) 线性化后的 path 序列，供上下篇
}

// Entry 按相对路径查条目。
func (c *Catalog) Entry(rel string) (Entry, bool) {
	if e, ok := c.byPath[rel]; ok {
		return *e, true
	}
	return Entry{}, false
}

// Category 按名字查分类。
func (c *Catalog) Category(name string) (Category, bool) {
	for _, cat := range c.Categories {
		if cat.Name == name {
			return cat, true
		}
	}
	return Category{}, false
}

// Neighbors 返回同分类内的上一篇/下一篇（跨分类不跳，避免读到一半掉进别的主题）。
func (c *Catalog) Neighbors(rel string) (prev, next *Entry) {
	e, ok := c.byPath[rel]
	if !ok {
		return nil, nil
	}
	for _, cat := range c.Categories {
		if cat.Name != e.Category {
			continue
		}
		for i := range cat.Entries {
			if cat.Entries[i].Path != rel {
				continue
			}
			if i > 0 {
				p := cat.Entries[i-1]
				prev = &p
			}
			if i+1 < len(cat.Entries) {
				n := cat.Entries[i+1]
				next = &n
			}
			return prev, next
		}
	}
	return nil, nil
}

// Paths 返回线性化后的全部条目路径（调试与测试用）。
func (c *Catalog) Paths() []string {
	out := make([]string, len(c.seq))
	copy(out, c.seq)
	return out
}
