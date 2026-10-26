package render

import "strings"

// wrapTables 给每个 <table> 套一层 div.table-wrap，并把列数写进 data-cols。
//
// 为什么不在 AST 层重写表格渲染器：goldmark 把 <tbody> 的开闭写在 KindTable 的
// 处理函数里，自己重写极易漏掉 </tbody>，浏览器会容错、测试也测不出来。
// 这里改成"先渲染、后包装、列数从 <thead> 里数"，行为稳定且可以直接断言。
func wrapTables(html string) string {
	if !strings.Contains(html, "<table>") {
		return html
	}
	var b strings.Builder
	rest := html
	for {
		i := strings.Index(rest, "<table>")
		if i < 0 {
			b.WriteString(rest)
			break
		}
		j := strings.Index(rest[i:], "</table>")
		if j < 0 {
			b.WriteString(rest)
			break
		}
		j += i

		b.WriteString(rest[:i])
		table := rest[i : j+len("</table>")]
		cols := countCols(table)
		b.WriteString(`<div class="table-wrap" data-cols="`)
		b.WriteString(itoa(cols))
		b.WriteString(`">`)
		b.WriteString(table)
		b.WriteString(`</div>`)
		rest = rest[j+len("</table>"):]
	}
	return b.String()
}

// countCols 数表头里的单元格个数。
//
// 注意：不能直接 strings.Count(s, "<th") —— "<thead>" 也以 "<th" 开头，
// 2 列的表格会被数成 3 列（这个 bug 就是被 render_test.go 抓到的）。
func countCols(table string) int {
	if i := strings.Index(table, "<thead>"); i >= 0 {
		if j := strings.Index(table[i:], "</thead>"); j >= 0 {
			return countCells(table[i:i+j], "th")
		}
	}
	if i := strings.Index(table, "<tbody>"); i >= 0 {
		if j := strings.Index(table[i:], "</tr>"); j >= 0 {
			return countCells(table[i:i+j], "td")
		}
	}
	return 0
}

// countCells 数 <th> / <th ...> 这类完整开标签的个数。
func countCells(s, tag string) int {
	open := "<" + tag
	n := 0
	for {
		i := strings.Index(s, open)
		if i < 0 {
			return n
		}
		rest := s[i+len(open):]
		// 开标签后面必须是 ">" 或空白（属性），否则是 <thead>/<tbody> 这类同前缀标签
		if rest != "" && (rest[0] == '>' || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n') {
			n++
		}
		s = rest
		if s == "" {
			return n
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
