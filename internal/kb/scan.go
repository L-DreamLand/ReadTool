package kb

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Options 控制扫描范围。
type Options struct {
	ShowWorkspace bool // 是否把 _workspace/ 里的产物也列出来
}

// Scan 遍历知识库，产出目录树。以文件系统为准，index.json 只做补充。
func Scan(root string, opt Options) (*Catalog, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("知识库路径不可用：%w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("知识库路径不是目录")
	}

	_, indexed, descs := loadIndex(rootAbs)

	cat := &Catalog{
		KBRoot:    rootAbs,
		RootFiles: []RootFile{},
		byPath:    map[string]*Entry{},
	}
	cats := map[string]*Category{}
	var maxMtime time.Time

	err = filepath.WalkDir(rootAbs, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// 单个目录不可读不该让整次扫描失败
			if errors.Is(walkErr, fs.ErrPermission) {
				return fs.SkipDir
			}
			return walkErr
		}
		name := d.Name()
		if d.IsDir() {
			if p == rootAbs {
				return nil
			}
			if strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			if name == "_workspace" && !opt.ShowWorkspace {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}

		fi, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(rootAbs, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		e := Entry{
			Path:    rel,
			Size:    fi.Size(),
			ModTime: fi.ModTime(),
		}
		if meta, ok := indexed[rel]; ok {
			e.Name = meta.Name
			e.Summary = meta.Summary
			e.Keywords = meta.Keywords
			e.Updated = meta.Updated
		} else {
			e.Unindexed = true
		}

		segs := strings.Split(rel, "/")
		if len(segs) == 1 {
			// 根目录文件：不进分类，单独列出
			e.Category = "根目录"
			kind := "other"
			switch strings.ToLower(segs[0]) {
			case "index.md":
				kind = "index"
			case "agents.md":
				kind = "agents"
			}
			if e.Name == "" {
				e.Name = strings.TrimSuffix(segs[0], filepath.Ext(segs[0]))
			}
			cat.RootFiles = append(cat.RootFiles, RootFile{Path: rel, Name: e.Name, Kind: kind})
			cat.byPath[rel] = &e
			if fi.ModTime().After(maxMtime) {
				maxMtime = fi.ModTime()
			}
			return nil
		}

		e.Category = segs[0]
		if e.Name == "" {
			e.Name = firstHeading(p)
		}
		if e.Name == "" {
			e.Name = strings.TrimSuffix(segs[len(segs)-1], filepath.Ext(segs[len(segs)-1]))
		}
		cat.Total++
		cat.byPath[rel] = &e
		if fi.ModTime().After(maxMtime) {
			maxMtime = fi.ModTime()
		}
		c, ok := cats[e.Category]
		if !ok {
			c = &Category{Name: e.Category, Desc: descs[e.Category]}
			cats[e.Category] = c
		}
		c.Entries = append(c.Entries, e)
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, c := range cats {
		// 稳定顺序：按 name 排，name 相同时按 path，避免每次扫描顺序漂移
		sort.SliceStable(c.Entries, func(i, j int) bool {
			if c.Entries[i].Name == c.Entries[j].Name {
				return c.Entries[i].Path < c.Entries[j].Path
			}
			return c.Entries[i].Name < c.Entries[j].Name
		})
		c.Count = len(c.Entries)
		for i := range c.Entries {
			c.Entries[i].order = i
			cat.seq = append(cat.seq, c.Entries[i].Path)
		}
		cat.Categories = append(cat.Categories, *c)
	}
	sort.Slice(cat.Categories, func(i, j int) bool { return cat.Categories[i].Name < cat.Categories[j].Name })
	sort.Slice(cat.RootFiles, func(i, j int) bool { return cat.RootFiles[i].Path < cat.RootFiles[j].Path })

	cat.Version = versionOf(maxMtime) + int64(cat.Total)
	return cat, nil
}

// firstHeading 只读文件开头的一小段，找到第一个 "# " 行就返回。
// 不能为了取个名字把 34 篇全文都读一遍。
func firstHeading(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024)
	read := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		read += len(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
		// 前 8KB 内没找到 H1 就放弃（README 类的长前言不该被全文扫描）
		if read > 8*1024 {
			break
		}
	}
	return ""
}

func versionOf(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
