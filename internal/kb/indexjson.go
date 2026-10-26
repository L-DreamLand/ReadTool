package kb

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// workspaceIndex 是 _workspace/index.json 里我们需要的那部分（只读，不写回）。
type workspaceIndex struct {
	Taxonomy []struct {
		Category string `json:"category"`
		Dir      string `json:"dir"`
		Desc     string `json:"desc"`
		Count    int    `json:"count"`
	} `json:"taxonomy"`
	Entries []struct {
		Name     string   `json:"name"`
		Category string   `json:"category"`
		Path     string   `json:"path"`
		Summary  string   `json:"summary"`
		Keywords []string `json:"keywords"`
		Updated  string   `json:"updated"`
	} `json:"entries"`
}

// loadIndex 读取 index.json。任何解析问题都退化为"没有元数据"，
// 因为知识库的磁盘状态才是唯一事实来源（index.json 可能滞后）。
func loadIndex(root string) (*workspaceIndex, map[string]*Entry, map[string]string) {
	entries := map[string]*Entry{}
	descs := map[string]string{}

	p := filepath.Join(root, "_workspace", "index.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, entries, descs
	}
	var idx workspaceIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, entries, descs
	}
	for _, t := range idx.Taxonomy {
		if t.Category != "" && t.Desc != "" {
			descs[t.Category] = t.Desc
		}
	}
	for _, e := range idx.Entries {
		rel := normalizeRel(e.Path)
		if rel == "" {
			continue
		}
		entries[rel] = &Entry{
			Path:     rel,
			Name:     e.Name,
			Category: e.Category,
			Summary:  e.Summary,
			Keywords: e.Keywords,
			Updated:  e.Updated,
		}
	}
	return &idx, entries, descs
}

// normalizeRel 把任意分隔符的路径统一成 KB 相对路径。
func normalizeRel(p string) string {
	r := filepath.ToSlash(filepath.Clean(p))
	for len(r) > 0 && (r[0] == '/' || r[0] == '.') {
		if len(r) > 1 && r[1] != '/' {
			break
		}
		r = r[1:]
	}
	if r == "." {
		return ""
	}
	return r
}
