// Package render 把 markdown 变成手机/平板上能直接看的 HTML 片段。
package render

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"

	"readtool/internal/kb"
)

// TOCItem 是正文的二级/三级标题。
type TOCItem struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Level int    `json:"level"`
}

// Result 是一次渲染的产物。
type Result struct {
	Path      string    `json:"path"`
	Title     string    `json:"title"`
	HTML      string    `json:"html"`
	TOC       []TOCItem `json:"toc"`
	WordCount int       `json:"wordCount"`
	Version   int64     `json:"version"`
}

// Options 构造渲染器。
type Options struct {
	Catalog   *kb.Catalog
	CacheSize int // 0 表示默认 200
}

type cached struct {
	res     Result
	version int64
}

// Renderer 带一层按 (mtime,size) 失效的缓存：34 篇全量常驻也只有几百 KB。
type Renderer struct {
	cat   *kb.Catalog
	root  string
	size  int
	mu    sync.Mutex
	mem   map[string]cached
	order []string // 简易 LRU：最早使用的排在前面
}

// New 创建渲染器。
func New(cat *kb.Catalog, opt Options) *Renderer {
	size := opt.CacheSize
	if size <= 0 {
		size = 200
	}
	return &Renderer{
		cat:  cat,
		root: cat.KBRoot,
		size: size,
		mem:  make(map[string]cached, size),
	}
}

// Render 渲染一篇条目。rel 必须是 KB 相对路径。
func (r *Renderer) Render(rel string) (Result, error) {
	abs, norm, err := kb.Resolve(r.root, rel)
	if err != nil {
		return Result{}, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return Result{}, err
	}
	v := fi.ModTime().UnixMilli()<<20 | (fi.Size() & 0xFFFFF)

	r.mu.Lock()
	if c, ok := r.mem[norm]; ok && c.version == v {
		r.touch(norm)
		r.mu.Unlock()
		return c.res, nil
	}
	r.mu.Unlock()

	src, err := os.ReadFile(abs)
	if err != nil {
		return Result{}, err
	}

	// 每次渲染用独立的 state：标题 id 去重与 TOC 都是文档级的
	st := &state{path: norm, cat: r.cat}
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(
			renderer.WithNodeRenderers(util.Prioritized(st, 100)),
		),
	)
	var buf bytes.Buffer
	if err := md.Convert(src, &buf); err != nil {
		return Result{}, fmt.Errorf("渲染失败：%w", err)
	}

	body := wrapTables(buf.String())
	title := st.title
	if title == "" {
		if e, ok := r.cat.Entry(norm); ok {
			title = e.Name
		} else {
			title = path.Base(norm)
		}
	}

	res := Result{
		Path:      norm,
		Title:     title,
		HTML:      body,
		TOC:       st.toc,
		WordCount: WordCount(string(src)),
		Version:   v,
	}
	if res.TOC == nil {
		res.TOC = []TOCItem{}
	}

	r.mu.Lock()
	r.mem[norm] = cached{res: res, version: v}
	r.touch(norm)
	for len(r.order) > r.size {
		oldest := r.order[0]
		r.order = r.order[1:]
		delete(r.mem, oldest)
	}
	r.mu.Unlock()
	return res, nil
}

func (r *Renderer) touch(rel string) {
	for i, p := range r.order {
		if p == rel {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	r.order = append(r.order, rel)
}

// state 实现 Heading 与 Link 两类节点的渲染（只覆盖这两类，表格交给默认渲染器）。
type state struct {
	path  string
	cat   *kb.Catalog
	toc   []TOCItem
	title string
	ids   map[string]int
}

func (s *state) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHeading, s.renderHeading)
	reg.Register(ast.KindLink, s.renderLink)
	registerCodeRenderers(reg, s)
}

func (s *state) renderHeading(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	h := n.(*ast.Heading)
	if entering {
		text := headingText(h, src)
		id := s.uniqueSlug(slug(text))
		if h.Level == 1 && s.title == "" {
			s.title = text
		}
		if h.Level == 2 || h.Level == 3 {
			s.toc = append(s.toc, TOCItem{ID: id, Text: text, Level: h.Level})
		}
		_, _ = fmt.Fprintf(w, `<h%d id="%s">`, h.Level, html.EscapeString(id))
	} else {
		_, _ = fmt.Fprintf(w, "</h%d>\n", h.Level)
	}
	return ast.WalkContinue, nil
}

func (s *state) renderLink(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("</a>")
		return ast.WalkContinue, nil
	}
	link := n.(*ast.Link)
	dest := string(link.Destination)
	href, broken := s.rewrite(dest)

	attrs := ` href="` + html.EscapeString(href) + `"`
	if len(link.Title) > 0 {
		attrs += ` title="` + html.EscapeString(string(link.Title)) + `"`
	}
	if broken {
		attrs += ` class="link-broken" data-broken="1"`
	}
	if isExternal(dest) {
		attrs += ` target="_blank" rel="noopener noreferrer"`
	}
	_, _ = w.WriteString("<a" + attrs + ">")
	return ast.WalkContinue, nil
}

// rewrite 把 25 处同目录兄弟链接（以及将来可能出现的其他站内链接）改写成前端路由。
// 目标不存在时保留文字并打标记——链接不该被悄悄丢掉。
func (s *state) rewrite(dest string) (string, bool) {
	dest = strings.TrimSpace(dest)
	if dest == "" || strings.HasPrefix(dest, "#") {
		return dest, false
	}
	u, err := url.Parse(dest)
	if err != nil {
		return dest, true
	}
	if u.Scheme != "" || strings.HasPrefix(dest, "//") {
		return dest, false
	}

	target := path.Clean(path.Join(path.Dir(s.path), u.Path))
	target = strings.TrimPrefix(target, "/")
	if s.cat != nil {
		if _, ok := s.cat.Entry(target); !ok {
			// 未登记但文件存在的链接也算能打开（例如目录里的非 md 附件）
			if _, _, err := kb.Resolve(s.cat.KBRoot, target); err != nil {
				href := "/doc/" + escapeSegments(target)
				if u.Fragment != "" {
					href += "#" + u.Fragment
				}
				return href, true
			}
		}
	}
	href := "/doc/" + escapeSegments(target)
	if u.Fragment != "" {
		href += "#" + u.Fragment
	}
	return href, false
}

func isExternal(dest string) bool {
	if strings.HasPrefix(dest, "//") {
		return true
	}
	u, err := url.Parse(dest)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// escapeSegments 对每一段单独编码：中文与空格都能往返，"/" 保持原样。
func escapeSegments(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

func (s *state) uniqueSlug(base string) string {
	if s.ids == nil {
		s.ids = map[string]int{}
	}
	if base == "" {
		base = "section"
	}
	s.ids[base]++
	if n := s.ids[base]; n > 1 {
		return fmt.Sprintf("%s-%d", base, n)
	}
	return base
}

// slug 保留汉字与字母数字，空格转 '-'，其余标点丢弃。
func slug(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_' || r == '·':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func headingText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(src))
		case *ast.String:
			b.Write(t.Value)
		case *ast.CodeSpan:
			// CodeSpan 的文本在子节点里，交给下面的 Text 分支
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(b.String())
}

// WordCount：中文按字、英文按词。
func WordCount(s string) int {
	n := 0
	inWord := false
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			n++
			inWord = false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if !inWord {
				n++
				inWord = true
			}
		default:
			inWord = false
		}
	}
	return n
}
