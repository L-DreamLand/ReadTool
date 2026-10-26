package kb

import (
	"os"
	"path/filepath"
	"testing"
)

// 知识库实测基线（需求文档 §2）：扫描当时是 34 条 / 7 个分类。
// 这是整份计划里性价比最高的一个测试：一次抓路径、编码、过滤三类 bug。
//
// 断言"下界 + 不变量"而不是精确数字：知识库会继续长（用户往里面加文件是常态），
// 精确计数每次都让测试变红，反而掩盖真正的问题。
// realKBRoot 返回"真实知识库"的路径，仅用于本地实测；没设环境变量就返回空，
// 相关用例会自动跳过 —— 不把任何人的本地路径写进要公开的仓库。
func realKBRoot() string { return os.Getenv("READTOOL_REAL_KB") }

func TestScanRealKB(t *testing.T) {
	if _, err := os.Stat(realKBRoot()); err != nil {
		t.Skip("本机没有知识库，跳过实测")
	}
	c, err := Scan(realKBRoot(), Options{})
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	if c.Total < 34 {
		t.Errorf("条目数 = %d，期望至少 34", c.Total)
	}
	if len(c.Categories) < 7 {
		t.Errorf("分类数 = %d，期望至少 7", len(c.Categories))
	}
	// 编程语言是最大分类（基线 14 条）
	seenMax := false
	for _, cat := range c.Categories {
		if cat.Name == "编程语言" {
			seenMax = true
			if cat.Count < 14 {
				t.Errorf("编程语言 = %d 条，期望至少 14", cat.Count)
			}
		}
		if cat.Count != len(cat.Entries) {
			t.Errorf("%s: count=%d 但 entries=%d", cat.Name, cat.Count, len(cat.Entries))
		}
	}
	if !seenMax {
		t.Error("没有找到「编程语言」分类，分类切分可能坏了")
	}
	// 根目录只应有 INDEX.md 与 AGENTS.md
	if len(c.RootFiles) < 2 {
		t.Errorf("根目录文件 = %d，期望至少 2 个", len(c.RootFiles))
	}
	kinds := map[string]bool{}
	for _, rf := range c.RootFiles {
		kinds[rf.Kind] = true
	}
	if !kinds["index"] || !kinds["agents"] {
		t.Errorf("INDEX.md/AGENTS.md 未正确识别：%+v", c.RootFiles)
	}
	// 每篇都该有显示名
	for _, cat := range c.Categories {
		for _, e := range cat.Entries {
			if e.Name == "" {
				t.Errorf("%s 没有显示名", e.Path)
			}
		}
	}
	if c.Version == 0 {
		t.Error("version 不应为 0")
	}
}

func TestScanSkipsHiddenAndWorkspace(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("分类A/一.md", "# 一\n")
	mk("分类A/二.md", "没有 H1 的文件\n")
	mk("_workspace/产物.md", "# 产物\n")
	mk(".git/忽略.md", "# 忽略\n")
	mk("分类A/notes.txt", "不是 markdown\n")

	c, err := Scan(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Total != 2 {
		t.Errorf("total = %d，期望 2（跳过 _workspace 与隐藏目录、非 md）", c.Total)
	}
	if _, ok := c.Entry("_workspace/产物.md"); ok {
		t.Error("_workspace 默认应被跳过")
	}
	if _, ok := c.Entry("分类A/二.md"); !ok {
		t.Error("缺 H1 的文件也应是条目")
	}

	c2, err := Scan(root, Options{ShowWorkspace: true})
	if err != nil {
		t.Fatal(err)
	}
	if c2.Total != 3 {
		t.Errorf("show_workspace 时 total = %d，期望 3", c2.Total)
	}
}

func TestNeighbors(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a.md", "b.md", "c.md"} {
		p := filepath.Join(root, "分类", n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := Scan(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	prev, next := c.Neighbors("分类/a.md")
	if prev != nil {
		t.Errorf("第一篇不该有上一篇：%+v", prev)
	}
	if next == nil || next.Path != "分类/b.md" {
		t.Errorf("第一篇的下一篇应为 b.md：%+v", next)
	}
	prev, next = c.Neighbors("分类/c.md")
	if prev == nil || prev.Path != "分类/b.md" {
		t.Errorf("最后一篇的上一篇应为 b.md：%+v", prev)
	}
	if next != nil {
		t.Errorf("最后一篇不该有下一篇：%+v", next)
	}
}

func TestResolveRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Resolve(root, "../secret.md"); err == nil {
		t.Error("上跳路径必须被拒绝")
	}
	if _, _, err := Resolve(root, "/abs.md"); err == nil {
		t.Error("绝对路径必须被拒绝")
	}
	if _, _, err := Resolve(root, "C:/Windows/win.ini"); err == nil {
		t.Error("盘符路径必须被拒绝")
	}
	abs, norm, err := Resolve(root, "ok.md")
	if err != nil || norm != "ok.md" || abs == "" {
		t.Errorf("正常路径应解析成功：%v %s %s", err, abs, norm)
	}
	if _, _, err := Resolve(root, "nope.md"); err != ErrNotFound {
		t.Errorf("不存在的路径应返回 ErrNotFound，实际 %v", err)
	}
}
