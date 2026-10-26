// @ts-check
package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"readtool/internal/kb"
)

// realKBRoot 返回"真实知识库"的路径，仅用于本地实测；没设环境变量就返回空，
// 相关用例会自动跳过 —— 不把任何人的本地路径写进要公开的仓库。
func realKBRoot() string { return os.Getenv("READTOOL_REAL_KB") }

func TestTerms(t *testing.T) {
	got := terms("泛型 Generics 中文")
	// 至少要有 bigram 和英文词
	set := make(map[string]bool, len(got))
	for _, g := range got {
		set[g] = true
	}
	for _, need := range []string{"泛型", "generics", "中文"} {
		if !set[need] {
			t.Fatalf("terms(%q) 缺少 %q，得到 %v", "泛型 Generics 中文", need, got)
		}
	}
	// 不应出现整句或带空格的东西
	for _, g := range got {
		if strings.ContainsAny(g, " \t\n") {
			t.Fatalf("词里不该有空白：%q", g)
		}
	}
}

func TestSearchTinyKB(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("语言/go.md", "# Go 语言\n\n从 1.18 起支持泛型：类型参数 + 约束。并发用 goroutine 与 channel。\n")
	write("语言/java.md", "# Java\n\nJDK 21 引入虚拟线程（Virtual Threads），把阻塞代码映射到少量平台线程。\n")
	write("AI/mcp.md", "# MCP\n\nMCP（Model Context Protocol）是工具协议。\n")

	cat := mustScan(t, root)
	idx, err := Build(cat)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	cases := []struct {
		name    string
		query   string
		wantTop string
		wantHit bool
	}{
		{"中文 bigram", "泛型", "语言/go.md", true},
		{"不连续的词", "虚拟线程", "语言/java.md", true},
		{"英文大写", "GOROUTINE", "语言/go.md", true},
		{"纯英文缩写", "mcp", "AI/mcp.md", true},
		{"单字", "羊", "", false},
		{"乱码无结果", "zzzzqqqq", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := idx.Search(Query{Text: c.query})
			if !c.wantHit {
				if res.Total != 0 {
					t.Fatalf("%q 应该没有结果，实际 %d 条：%+v", c.query, res.Total, res.Hits)
				}
				return
			}
			if res.Total == 0 || len(res.Hits) == 0 {
				t.Fatalf("%q 应该有结果", c.query)
			}
			if res.Hits[0].Path != c.wantTop {
				t.Fatalf("%q 第一条应为 %s，实际 %s（全部：%+v）", c.query, c.wantTop, res.Hits[0].Path, res.Hits)
			}
		})
	}
}

func TestSearchFiltersAndSnippet(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("语言/go.md", "# Go\n\n"+strings.Repeat("前缀文字。", 30)+"泛型与类型参数在这里出现。\n")
	write("AI/go.md", "# Go 生态\n\n这里只提泛型一次。\n")

	cat := mustScan(t, root)
	idx, err := Build(cat)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	all := idx.Search(Query{Text: "泛型"})
	if all.Total != 2 {
		t.Fatalf("应有 2 条，实际 %d：%+v", all.Total, all.Hits)
	}

	only := idx.Search(Query{Text: "泛型", Category: "语言"})
	if only.Total != 1 || only.Hits[0].Path != "语言/go.md" {
		t.Fatalf("分类过滤失效：%+v", only.Hits)
	}

	title := idx.Search(Query{Text: "泛型", TitleOnly: true})
	if title.Total != 0 {
		t.Fatalf("仅标题时不该命中正文里的词：%+v", title.Hits)
	}

	// 片段要包含命中词并带 <em>，且命中在长正文中部时前面要有省略号
	var snip string
	for _, h := range all.Hits {
		if h.Path == "语言/go.md" {
			snip = h.Snippet
		}
	}
	if snip == "" {
		t.Fatalf("没找到 语言/go.md 的命中：%+v", all.Hits)
	}
	if !strings.Contains(snip, "<em>泛型</em>") {
		t.Fatalf("片段未高亮：%q", snip)
	}
	if !strings.HasPrefix(snip, "…") {
		t.Fatalf("命中在正文中部时应以前缀省略号开头：%q", snip)
	}

	// 片段必须转义 HTML：正文里出现 <script> 不能原样带出去
	write2 := filepath.Join(root, "语言/html.md")
	if err := os.WriteFile(write2, []byte("# HTML\n\n<script>alert(1)</script> 与泛型无关。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat2 := mustScan(t, root)
	idx2, err := Build(cat2)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	res := idx2.Search(Query{Text: "script"})
	for _, h := range res.Hits {
		if strings.Contains(h.Snippet, "<script>") {
			t.Fatalf("片段里出现了未转义的 <script>：%q", h.Snippet)
		}
	}
}

func TestLimitClamp(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		name := filepath.Join(root, "c", "n"+string(rune('0'+i))+".md")
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("# 条目\n\n共同词：泛型。\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cat := mustScan(t, root)
	idx, err := Build(cat)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := len(idx.Search(Query{Text: "泛型", Limit: 2}).Hits); got != 2 {
		t.Fatalf("limit=2 应只回 2 条，实际 %d", got)
	}
	if got := idx.Search(Query{Text: "泛型"}).Total; got != 5 {
		t.Fatalf("total 应为命中条目数 5，实际 %d", got)
	}
}

// TestSearchRealKB 用真实知识库验证 AC-09 的四条硬要求。
func TestSearchRealKB(t *testing.T) {
	if _, err := os.Stat(realKBRoot()); err != nil {
		t.Skip("没有真实知识库，跳过")
	}
	cat, err := kb.Scan(realKBRoot(), kb.Options{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	start := time.Now()
	idx, err := Build(cat)
	buildTook := time.Since(start)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if buildTook > 300*time.Millisecond {
		t.Fatalf("建索引耗时 %v，超过 AC-09-6 的 300ms", buildTook)
	}
	t.Logf("建索引：%d 篇，耗时 %v", len(idx.docs), buildTook)

	check := func(query, wantPath string) {
		t.Helper()
		res := idx.Search(Query{Text: query})
		if res.Total == 0 {
			t.Fatalf("%q 没有命中任何条目", query)
		}
		found := false
		for _, h := range res.Hits {
			if h.Path == wantPath {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%q 应命中 %s，实际：%+v", query, wantPath, res.Hits)
		}
	}

	check("泛型", "编程语言/go.md")
	check("虚拟线程", "编程语言/java.md")
	check("goroutine", "编程语言/go.md")

	// AC-09-3：MCP 应排第一
	mcp := idx.Search(Query{Text: "MCP"})
	if mcp.Total == 0 || mcp.Hits[0].Path != "人工智能/mcp.md" {
		t.Fatalf("MCP 的第一条应为 人工智能/mcp.md，实际：%+v", mcp.Hits)
	}

	// 片段里必须真的高亮命中词，且整串作为一个 <em>（不是拆成 M / CP）
	snip := mcp.Hits[0].Snippet
	if !strings.Contains(snip, "<em>MCP</em>") {
		t.Fatalf("MCP 的片段没有整串高亮：%q", snip)
	}
	// Markdown 噪声不该漏进片段
	if strings.HasPrefix(snip, "#") || strings.HasPrefix(snip, "… #") {
		t.Fatalf("片段开头带了 Markdown 标记：%q", snip)
	}
	if strings.Contains(snip, "\n") {
		t.Fatalf("片段里不该有换行：%q", snip)
	}
}

func mustScan(t *testing.T, root string) *kb.Catalog {
	t.Helper()
	cat, err := kb.Scan(root, kb.Options{})
	if err != nil {
		t.Fatalf("Scan(%s): %v", root, err)
	}
	return cat
}
