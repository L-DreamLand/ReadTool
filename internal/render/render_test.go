package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"readtool/internal/kb"
)

// realKBRoot 返回"真实知识库"的路径，仅用于本地实测；没设环境变量就返回空，
// 相关用例会自动跳过 —— 不把任何人的本地路径写进要公开的仓库。
func realKBRoot() string { return os.Getenv("READTOOL_REAL_KB") }

func TestWrapTablesCountsColumns(t *testing.T) {
	in := `<table><thead><tr><th>a</th><th>b</th></tr></thead><tbody><tr><td>1</td><td>2</td></tr></tbody></table>`
	out := wrapTables(in)
	if !strings.HasPrefix(out, `<div class="table-wrap" data-cols="2">`) {
		t.Fatalf("包装结果不对：%s", out)
	}
	if !strings.HasSuffix(out, `</table></div>`) {
		t.Fatalf("缺少收尾标签：%s", out)
	}
	// 没有表格时原样返回
	plain := `<p>没有表格</p>`
	if got := wrapTables(plain); got != plain {
		t.Errorf("无表格时不应改动内容：%s", got)
	}
}

func TestWordCount(t *testing.T) {
	cases := map[string]int{
		"中文四个字":       5,
		"hello world": 2,
		"Go 语言 v1.27": 5, // 版本号按 "v1" + "27" 两个词算，不复刻语义
		"":            0,
	}
	for in, want := range cases {
		if got := WordCount(in); got != want {
			t.Errorf("WordCount(%q) = %d，期望 %d", in, got, want)
		}
	}
}

func TestSlug(t *testing.T) {
	if got := slug("二级标题 A"); got != "二级标题-a" {
		t.Errorf("slug = %q", got)
	}
	if got := slug("Hello, World!"); got != "hello-world" {
		t.Errorf("slug = %q", got)
	}
	if got := slug("a  b"); got != "a-b" {
		t.Errorf("连续空格应折叠成一个连字符：%q", got)
	}
}

// 用真实知识库里最大的一篇做端到端渲染校验。
func TestRenderRealDoc(t *testing.T) {
	if _, err := os.Stat(realKBRoot()); err != nil {
		t.Skip("本机没有知识库，跳过实测")
	}
	cat, err := kb.Scan(realKBRoot(), kb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := New(cat, Options{})

	rel := "人工智能/技能.md"
	if _, ok := cat.Entry(rel); !ok {
		t.Skipf("%s 不存在，跳过", rel)
	}
	res, err := r.Render(rel)
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	if len(res.HTML) == 0 {
		t.Fatal("渲染结果为空")
	}
	if len(res.TOC) < 3 {
		t.Errorf("TOC 只有 %d 项，期望至少 3", len(res.TOC))
	}
	if !strings.Contains(res.HTML, `<div class="table-wrap" data-cols="`) {
		t.Error("表格没有被包装")
	}
	if res.Title == "" {
		t.Error("标题为空")
	}
	if res.WordCount == 0 {
		t.Error("字数统计为 0")
	}

	// 第二次必须命中缓存（版本号相同）
	res2, err := r.Render(rel)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Version != res.Version {
		t.Error("同一文件两次渲染版本应一致")
	}
}

// 渲染一篇临时文档，检查标题 id 去重与链接改写。
func TestRenderHeadingIDsAndLinks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "分类")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `# 标题

## 重复
## 重复
## 重复

[兄弟](other.md) [外链](https://example.com) [锚点](#重复) [坏的](nope.md)
`
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.md"), []byte("# 另一篇\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := kb.Scan(root, kb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := New(cat, Options{}).Render("分类/a.md")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{`id="重复"`, `id="重复-2"`, `id="重复-3"`} {
		if !strings.Contains(res.HTML, want) {
			t.Errorf("标题 id 去重失败，缺少 %s：%s", want, res.HTML)
		}
	}
	if !strings.Contains(res.HTML, `href="/doc/%E5%88%86%E7%B1%BB/other.md"`) {
		t.Errorf("兄弟链接未改写：%s", res.HTML)
	}
	if !strings.Contains(res.HTML, `href="https://example.com" target="_blank"`) {
		t.Errorf("外链应新窗口打开：%s", res.HTML)
	}
	if !strings.Contains(res.HTML, `href="#重复"`) {
		t.Errorf("页内锚点应保持原样：%s", res.HTML)
	}
	if !strings.Contains(res.HTML, `data-broken="1"`) {
		t.Errorf("坏链接应被标记：%s", res.HTML)
	}
	if res.Title != "标题" {
		t.Errorf("title = %q", res.Title)
	}
}

func TestRenderCodeBlocks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "分类")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "## 代码\n\n```go\nfunc main() {\n\tfmt.Println(\"<T>\")\n}\n```\n\n" +
		"```某个不存在的语言\nplain text here\n```\n\n" +
		"    缩进代码块\n    第二行\n"
	if err := os.WriteFile(filepath.Join(dir, "c.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := kb.Scan(root, kb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := New(cat, Options{}).Render("分类/c.md")
	if err != nil {
		t.Fatal(err)
	}
	h := res.HTML

	for _, want := range []string{
		`<div class="code-block" data-lang="go">`,
		`<span class="code-lang">Go</span>`,
		`class="code-copy"`,
		`<pre class="chroma"><code>`,
		`<span class="kd">func</span>`, // chroma 的关键字 class
		`&lt;T&gt;`,                    // 代码内容必须转义
		`data-lang="plaintext"`,        // 认不出的语言退化成纯文本
	} {
		if !strings.Contains(h, want) {
			t.Errorf("代码块缺少 %s\n%s", want, h)
		}
	}
	// 空语言也算纯文本，且不能把语言名照抄进 data-lang
	if strings.Contains(h, `data-lang="某个不存在的语言"`) {
		t.Errorf("未知语言不应原样进 data-lang：%s", h)
	}
	// 缩进代码块也要包一层，否则它没有复制按钮
	if !strings.Contains(h, `data-lang="plaintext"`) || !strings.Contains(h, "缩进代码块") {
		t.Errorf("缩进代码块未处理：%s", h)
	}
}

func TestChromaCSSHasBothThemes(t *testing.T) {
	css := ChromaCSS()
	if !strings.Contains(css, `:root[data-theme="dark"] .chroma`) {
		t.Errorf("缺少暗色配色：%s", css[:min(400, len(css))])
	}
	if !strings.Contains(css, "prefers-color-scheme: dark") {
		t.Errorf("auto 模式缺少跟随系统的配色")
	}
	if !strings.Contains(css, ".chroma .k") && !strings.Contains(css, ".chroma .kd") {
		t.Errorf("缺少 token 配色规则")
	}
	// 不应把 chroma 自带背景整块带过来，否则和我们自己的 --code-bg 打架
	if strings.Contains(css, ".chroma {\n  background-color") {
		t.Errorf("不应保留 chroma 的基类背景规则")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
