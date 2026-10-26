package render

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// 代码块（FR-16）。
//
// 结构固定为：外层 .code-block 带 data-lang，头部一个语言标签 + 复制按钮，
// 主体是 chroma 生成的 <pre class="chroma"><code>。
// 复制按钮在前端用事件委托处理——服务端不塞脚本（CSP 干净、也不需要构建）。
func (s *state) renderFencedCode(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	fcb, ok := n.(*ast.FencedCodeBlock)
	if !ok {
		return ast.WalkContinue, nil
	}
	lang := strings.ToLower(string(fcb.Language(src)))
	writeCodeBlock(w, codeText(n, src), lang)
	return ast.WalkSkipChildren, nil
}

// 缩进式代码块（四个空格开头）没有语言标注，按纯文本处理。
func (s *state) renderCodeBlock(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	writeCodeBlock(w, codeText(n, src), "")
	return ast.WalkSkipChildren, nil
}

// escHTML 转义 HTML 特殊字符（代码内容必须转义后再进 <code>）。
func escHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// codeText 取代码块原文。goldmark 把内容按行存成 segment，需要逐行拼回来；
// 不能用 node.Text()——它会把行尾换行吃掉，粘贴出来的代码就没有换行了。
func codeText(n ast.Node, src []byte) string {
	var buf bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		buf.Write(seg.Value(src))
	}
	return buf.String()
}

// 语言别名归一：KB 里常见 ts / js / sh / yml / c++ 这类写法。
var langAlias = map[string]string{
	"js": "javascript", "jsx": "javascript", "ts": "typescript", "tsx": "typescript",
	"sh": "bash", "shell": "bash", "zsh": "bash", "console": "bash",
	"yml": "yaml", "toml": "ini", "ps1": "powershell", "pwsh": "powershell",
	"c++": "cpp", "c#": "csharp", "cs": "csharp", "golang": "go",
	"html": "html", "htm": "html", "md": "markdown", "jsonc": "json",
	"text": "plaintext", "txt": "plaintext", "": "plaintext",
}

// 语言标签在头部显示成人类可读的写法。
var langLabel = map[string]string{
	"go": "Go", "python": "Python", "javascript": "JavaScript", "typescript": "TypeScript",
	"bash": "Bash", "powershell": "PowerShell", "yaml": "YAML", "json": "JSON",
	"toml": "TOML", "ini": "INI", "sql": "SQL", "html": "HTML", "css": "CSS",
	"dockerfile": "Dockerfile", "markdown": "Markdown", "plaintext": "文本",
	"java": "Java", "rust": "Rust", "cpp": "C++", "csharp": "C#", "c": "C",
	"jsx": "JSX", "tsx": "TSX", "nginx": "Nginx", "makefile": "Makefile",
}

// codeStyle 用固定的两张样式：亮色 github、暗色 github-dark，与站点的两套主题配套。
var (
	cssCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	lightStyle   = styles.Get("github")
	darkStyle    = styles.Get("github-dark")
	htmlFmt      = html.New(html.WithClasses(true), html.WithLineNumbers(false))
)

// writeCodeBlock 生成一个完整的代码块。lang 为空（或认不出）时按纯文本输出，
// 这样至少不会把缩进和换行搞乱，也不会因为找不到 lexer 而 panic。
func writeCodeBlock(w util.BufWriter, code, lang string) {
	norm := langAlias[lang]
	if norm == "" {
		norm = lang
	}
	// 认不出的语言名按纯文本处理：保留原样只会显示成一串没人认识的标签
	if norm != "" && norm != "plaintext" && lexers.Get(norm) == nil && lexers.Match(norm) == nil {
		norm = "plaintext"
	}
	label := langLabel[norm]
	if label == "" {
		label = "文本"
	}

	code = strings.TrimRight(code, "\n")

	// 空代码块（```lang``` 里什么都没有）也要有个可见的框，别渲染成零高度
	if strings.TrimSpace(code) == "" {
		_, _ = w.WriteString(`<div class="code-block" data-lang="` + escHTML(norm) + `">` +
			`<div class="code-head"><span class="code-lang">` + escHTML(label) + `</span>` +
			`<button class="code-copy" type="button">复制</button></div>` +
			`<pre class="chroma"><code></code></pre></div>`)
		return
	}

	lexer := lexers.Get(norm)
	if lexer == nil {
		lexer = lexers.Match(norm)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	var buf bytes.Buffer
	buf.WriteString(`<div class="code-block" data-lang="` + escHTML(norm) + `">`)
	buf.WriteString(`<div class="code-head"><span class="code-lang">` + escHTML(label) + `</span>`)
	buf.WriteString(`<button class="code-copy" type="button">复制</button></div>`)

	it, err := lexer.Tokenise(nil, code)
	if err != nil || htmlFmt.Format(&buf, lightStyle, it) != nil {
		// 高亮失败就退回纯文本，绝不能让一篇笔记因为一个代码块整页打不开
		buf.Reset()
		buf.WriteString(`<div class="code-block" data-lang="` + escHTML(norm) + `">`)
		buf.WriteString(`<div class="code-head"><span class="code-lang">` + escHTML(label) + `</span>`)
		buf.WriteString(`<button class="code-copy" type="button">复制</button></div>`)
		buf.WriteString(`<pre class="chroma"><code>` + escHTML(code) + `</code></pre></div>`)
		_, _ = w.WriteString(buf.String())
		return
	}
	buf.WriteString(`</div>`)
	_, _ = w.WriteString(buf.String())
}

// ChromaCSS 生成高亮所需的两套配色。
//
// 只输出 token 颜色（去掉 chroma 自带的那条 .chroma{background-color:…}），
// 背景交给 markdown.css 用 --code-bg 统一控制，深浅主题切换时不会打架。
func ChromaCSS() string {
	light := chromaTokenCSS(lightStyle)
	dark := chromaTokenCSS(darkStyle)

	var b strings.Builder
	b.WriteString("/* chroma token 配色：由 internal/render/code.go 运行时生成，不要手改 */\n")
	b.WriteString(light)
	b.WriteString(prefixSelectors(dark, `:root[data-theme="dark"] `))
	// auto 模式跟随系统
	b.WriteString("@media (prefers-color-scheme: dark) {\n")
	b.WriteString(prefixSelectors(dark, `:root[data-theme="auto"] `))
	b.WriteString("}\n")
	return b.String()
}

// chromaTokenCSS 把样式渲染成 CSS，并丢掉基类背景规则。
//
// 注意：chroma 的 WriteCSS 会把注释和选择器写在**同一行**
// （`/* Background */ .bg { … }`），所以必须先剥注释再按行处理，
// 否则前缀会加到注释后面，生成 `:root[data-theme="dark"] /* … */ .bg` 这种废规则。
func chromaTokenCSS(style *chroma.Style) string {
	if style == nil {
		style = styles.Fallback
	}
	var buf bytes.Buffer
	if err := htmlFmt.WriteCSS(&buf, style); err != nil {
		return ""
	}
	raw := cssCommentRe.ReplaceAllString(buf.String(), "")

	out := make([]string, 0, 40)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, " ")
		if strings.TrimSpace(line) == "" {
			continue
		}
		sel := strings.TrimSpace(line)
		if i := strings.Index(sel, "{"); i >= 0 {
			sel = strings.TrimSpace(sel[:i])
		}
		// 背景交给我们自己的 --code-bg：基类（.bg/.chroma）整条丢掉
		if sel == ".bg" || sel == ".chroma" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n") + "\n"
}

// prefixSelectors 给每条规则的选择器加前缀，用于把暗色配色限定在某个主题下。
func prefixSelectors(css, prefix string) string {
	var b strings.Builder
	for _, line := range strings.Split(css, "\n") {
		if idx := strings.Index(line, "{"); idx >= 0 && !strings.HasPrefix(strings.TrimSpace(line), "@") {
			sel := strings.TrimSpace(line[:idx])
			rest := line[idx:]
			parts := strings.Split(sel, ",")
			for i, p := range parts {
				parts[i] = prefix + strings.TrimSpace(p)
			}
			b.WriteString(strings.Join(parts, ", "))
			b.WriteString(" ")
			b.WriteString(rest)
			b.WriteString("\n")
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// 确保两个渲染器都被注册（在 state.RegisterFuncs 里调用）。
func registerCodeRenderers(reg renderer.NodeRendererFuncRegisterer, s *state) {
	reg.Register(ast.KindFencedCodeBlock, s.renderFencedCode)
	reg.Register(ast.KindCodeBlock, s.renderCodeBlock)
}
