// @ts-check
package search

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"readtool/internal/kb"
)

/**
 * 全文搜索（FR-09）。
 *
 * 中文按 **bigram**（相邻两字）建索引，英文/数字按非字母数字切词并转小写；
 * 查询走同一条路：切词 → 倒排取候选 → 逐条子串校验（避免 bigram 假阳性）→ 权重打分。
 * 语料只有几十篇（0.4 MB），全量重建在毫秒级，所以不做增量（FR-09 规则 6 允许 v1 全量）。
 */

// 字段权重（FR-09 规则 3）
const (
	weightName     = 3.0
	weightKeywords = 2.0
	weightSummary  = 1.5
	weightBody     = 1.0
)

// 片段前后各取多少个字
const snippetRadius = 40

/** 一篇可被搜索的条目 */
type Doc struct {
	Path     string
	Name     string
	Category string
	Summary  string
	Keywords string
	Body     string

	nameLower     string
	summaryLower  string
	keywordsLower string
	bodyLower     string
}

/** 倒排索引 */
type Index struct {
	docs []Doc
	// term → doc 下标（升序、去重）
	postings map[string][]int32
	Built    time.Time
}

/** 一次查询 */
type Query struct {
	Text      string
	Category  string
	TitleOnly bool
	Limit     int
}

/** 一条命中 */
type Hit struct {
	Path     string  `json:"path"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Score    float64 `json:"score"`
	Count    int     `json:"count"`
	Snippet  string  `json:"snippet"`
}

/** 查询结果 */
type Result struct {
	Query string `json:"query"`
	Total int    `json:"total"`
	Took  int64  `json:"took"`
	Hits  []Hit  `json:"hits"`
}

// Build 读取目录里的全部条目正文并建索引。
// 读文件失败不致命：那一篇的正文按空处理（名字/摘要仍可搜索），错误聚合返回。
func Build(cat *kb.Catalog) (*Index, error) {
	entries := allEntries(cat)
	docs := make([]Doc, 0, len(entries))

	var firstErr error
	for _, e := range entries {
		body := ""
		abs, _, err := kb.Resolve(cat.KBRoot, e.Path)
		if err == nil {
			if data, readErr := os.ReadFile(abs); readErr == nil {
				body = string(data)
			} else if firstErr == nil {
				firstErr = readErr
			}
		} else if firstErr == nil {
			firstErr = err
		}
		docs = append(docs, newDoc(e, body))
	}

	idx := &Index{docs: docs, postings: make(map[string][]int32, 4096), Built: time.Now()}
	for i := range docs {
		idx.indexDoc(int32(i))
	}
	return idx, firstErr
}

func newDoc(e kb.Entry, body string) Doc {
	// 剥掉 BOM，避免首字搜不到
	body = strings.TrimPrefix(body, "\ufeff")
	// Entry.Keywords 是切片，拼成一段文本再切词（关键词权重是字段级的，不需要保留分组）
	keywords := strings.Join(e.Keywords, " ")
	return Doc{
		Path:          e.Path,
		Name:          e.Name,
		Category:      e.Category,
		Summary:       e.Summary,
		Keywords:      keywords,
		Body:          body,
		nameLower:     strings.ToLower(e.Name),
		summaryLower:  strings.ToLower(e.Summary),
		keywordsLower: strings.ToLower(keywords),
		bodyLower:     strings.ToLower(body),
	}
}

func allEntries(cat *kb.Catalog) []kb.Entry {
	if cat == nil {
		return nil
	}
	out := make([]kb.Entry, 0, cat.Total)
	for _, c := range cat.Categories {
		out = append(out, c.Entries...)
	}
	return out
}

func (idx *Index) indexDoc(i int32) {
	doc := &idx.docs[i]
	seen := make(map[string]struct{}, 128)
	add := func(terms []string) {
		for _, t := range terms {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			idx.postings[t] = append(idx.postings[t], i)
		}
	}
	add(terms(doc.Name))
	add(terms(doc.Keywords))
	add(terms(doc.Summary))
	add(terms(doc.Body))
}

// Search 执行查询。返回 Result 永远非 nil（无命中就是空数组）。
func (idx *Index) Search(q Query) Result {
	started := time.Now()
	text := strings.TrimSpace(q.Text)
	res := Result{Query: text, Hits: []Hit{}}
	if text == "" || idx == nil {
		res.Took = time.Since(started).Milliseconds()
		return res
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	queryTerms := terms(text)
	lower := strings.ToLower(text)

	candidates := idx.candidates(queryTerms)
	// 单字查询（如「羊」）没有 bigram，走全量扫描；语料只有几十篇，代价可忽略
	if len(candidates) == 0 {
		candidates = make([]int32, len(idx.docs))
		for i := range idx.docs {
			candidates[i] = int32(i)
		}
	}

	hits := make([]Hit, 0, len(candidates))
	for _, i := range candidates {
		doc := &idx.docs[i]
		if q.Category != "" && doc.Category != q.Category {
			continue
		}

		// 每个查询词都必须在某个字段里出现过（AND 语义），
		// 且该字段真的包含这段子串（bigram 只用来缩小候选集）
		names := 0
		keywords := 0
		summary := 0
		body := 0
		count := 0
		matchedAll := true
		for _, t := range queryTerms {
			n := countIn(doc.nameLower, t)
			k := countIn(doc.keywordsLower, t)
			s := countIn(doc.summaryLower, t)
			b := 0
			if !q.TitleOnly {
				b = countIn(doc.bodyLower, t)
			}
			if n+k+s+b == 0 {
				matchedAll = false
				break
			}
			names += n
			keywords += k
			summary += s
			body += b
			count += n + k + s + b
		}
		if !matchedAll || count == 0 {
			continue
		}

		score := weightName*float64(names) +
			weightKeywords*float64(keywords) +
			weightSummary*float64(summary) +
			weightBody*float64(body)
		// 整串连续命中再加一点分：搜「虚拟线程」时整词出现的那篇应该排在只散落两字的前面
		if strings.Contains(doc.nameLower, lower) || (!q.TitleOnly && strings.Contains(doc.bodyLower, lower)) {
			score += 2
		}

		hits = append(hits, Hit{
			Path:     doc.Path,
			Name:     doc.Name,
			Category: doc.Category,
			Score:    round1(score),
			Count:    count,
			Snippet:  doc.snippet(queryTerms, lower, q.TitleOnly),
		})
	}

	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].Score != hits[b].Score {
			return hits[a].Score > hits[b].Score
		}
		if hits[a].Count != hits[b].Count {
			return hits[a].Count > hits[b].Count
		}
		return hits[a].Path < hits[b].Path
	})

	res.Total = len(hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	res.Hits = hits
	res.Took = time.Since(started).Milliseconds()
	return res
}

// candidates 取所有查询词的倒排并集。
func (idx *Index) candidates(queryTerms []string) []int32 {
	if len(queryTerms) == 0 {
		return nil
	}
	var cands []int32
	for _, t := range queryTerms {
		post := idx.postings[t]
		if len(post) == 0 {
			// 有一个词完全不存在 → 不可能命中（AND 语义）
			return nil
		}
		if cands == nil {
			cands = append(cands, post...)
			continue
		}
		cands = intersect(cands, post)
		if len(cands) == 0 {
			return nil
		}
	}
	return cands
}

func intersect(a, b []int32) []int32 {
	out := make([]int32, 0, len(a))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, a[i])
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return out
}

func countIn(haystack, needle string) int {
	if needle == "" || haystack == "" {
		return 0
	}
	return strings.Count(haystack, needle)
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}

// snippet 从正文里截取命中位置前后各 snippetRadius 个字，命中词包 <em>。
// 正文没有命中（只命中标题/摘要）时退到摘要，再退到正文开头。
func (doc *Doc) snippet(queryTerms []string, lower string, titleOnly bool) string {
	source := doc.Body
	lowerSource := doc.bodyLower
	if titleOnly || strings.TrimSpace(lowerSource) == "" {
		source = doc.Summary
		lowerSource = doc.summaryLower
	}
	if strings.TrimSpace(source) == "" {
		return ""
	}

	// Markdown 正文直接截会给出一堆 # / | / ``` 噪声，先压成一行。
	// 压平后位置关系仍一致（都基于同一个 flat），所以后面可以安全按字节切片。
	flat := flatten(source)
	flatLower := strings.ToLower(flat)
	if len(flat) == 0 {
		return ""
	}

	// 优先用整串，其次用最长的词，定位片段中心
	anchor := -1
	anchorLen := 0
	if lower != "" {
		if p := strings.Index(flatLower, lower); p >= 0 {
			anchor, anchorLen = p, len(lower)
		}
	}
	if anchor < 0 {
		for _, t := range queryTerms {
			if t == "" {
				continue
			}
			if p := strings.Index(flatLower, t); p >= 0 && (anchor < 0 || len(t) > anchorLen) {
				anchor, anchorLen = p, len(t)
			}
		}
	}
	if anchor < 0 {
		anchor = 0
	}

	start := anchor - snippetRadius
	if start < 0 {
		start = 0
	}
	// 不要从字中间切开（UTF-8 续字节是 10xxxxxx）
	for start > 0 && start < len(flat) && flat[start]&0xC0 == 0x80 {
		start--
	}
	end := anchor + anchorLen + snippetRadius
	if end > len(flat) {
		end = len(flat)
	}
	for end < len(flat) && flat[end]&0xC0 == 0x80 {
		end++
	}

	chunk := strings.TrimLeft(flat[start:end], "#>|*`- \t")
	if chunk == "" {
		return ""
	}
	chunkLower := strings.ToLower(chunk)

	// 整串在片段里出现时只高亮整串：否则「虚拟线程」会被拆成「虚拟」「线程」两个高亮
	terms2 := queryTerms
	if lower != "" && strings.Contains(chunkLower, lower) {
		terms2 = []string{lower}
	}

	prefix := ""
	if start > 0 {
		prefix = "…"
	}
	suffix := ""
	if end < len(flat) {
		suffix = "…"
	}
	return prefix + highlight(chunk, chunkLower, terms2) + suffix
}

// flatten 把 Markdown 正文压成单行：换行与连续空白都变一个空格。
func flatten(s string) string {
	// 去掉 Markdown 噪声：链接/图片语法、行内强调与代码标记、表格竖线、
	// 行首的项目符号与树形连接符。这些字符进片段只会让人看不出重点。
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = mdImageRe.ReplaceAllString(s, "")
	s = mdTreeRe.ReplaceAllString(s, "")
	s = mdNoiseRe.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}

var (
	mdImageRe = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdLinkRe  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	// 行首的 "├── "、"│ "、"- "、"| " 之类，以及行内的 ** ` | 符号
	mdTreeRe  = regexp.MustCompile(`(?m)^[\s│├└─┌┐┘┬┴┼|>*\-+#]+`)
	mdNoiseRe = regexp.MustCompile("[`|]|\\*\\*")
)

// highlight 把 chunk 里所有命中词包上 <em>，其余部分做 HTML 转义。
func highlight(chunk, chunkLower string, queryTerms []string) string {
	if len(queryTerms) == 0 {
		return htmlEscape(chunk)
	}

	// 收集所有命中区间，合并重叠
	type span struct{ start, end int }
	spans := make([]span, 0, 8)
	for _, t := range queryTerms {
		if t == "" {
			continue
		}
		from := 0
		for {
			p := strings.Index(chunkLower[from:], t)
			if p < 0 {
				break
			}
			abs := from + p
			spans = append(spans, span{abs, abs + len(t)})
			from = abs + len(t)
			if from >= len(chunkLower) {
				break
			}
		}
	}
	if len(spans) == 0 {
		return htmlEscape(chunk)
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end > spans[j].end
	})

	var b strings.Builder
	pos := 0
	for _, sp := range spans {
		if sp.start < pos {
			if sp.end > pos {
				// 与前一个区间重叠：延长前一个 <em> 的结束位置
				b.WriteString(htmlEscape(chunk[pos:sp.end]))
				pos = sp.end
			}
			continue
		}
		b.WriteString(htmlEscape(chunk[pos:sp.start]))
		b.WriteString("<em>")
		b.WriteString(htmlEscape(chunk[sp.start:sp.end]))
		b.WriteString("</em>")
		pos = sp.end
	}
	b.WriteString(htmlEscape(chunk[pos:]))
	return b.String()
}

func htmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	).Replace(s)
}

// terms 切出索引/查询词：连续汉字 → 相邻二字 bigram（单字段也放单字）+ 英文数字词。
func terms(s string) []string {
	if s == "" {
		return nil
	}
	seen := make(map[string]struct{}, 64)
	out := make([]string, 0, 64)

	addRun := func(runes []rune) {
		if len(runes) == 0 {
			return
		}
		if len(runes) == 1 {
			t := string(runes)
			if _, ok := seen[t]; !ok {
				seen[t] = struct{}{}
				out = append(out, t)
			}
			return
		}
		for i := 0; i+1 < len(runes); i++ {
			t := string(runes[i : i+2])
			if _, ok := seen[t]; !ok {
				seen[t] = struct{}{}
				out = append(out, t)
			}
		}
	}

	var cjk []rune
	var word []rune
	flushWord := func() {
		if len(word) > 0 {
			t := strings.ToLower(string(word))
			if _, ok := seen[t]; !ok {
				seen[t] = struct{}{}
				out = append(out, t)
			}
			word = word[:0]
		}
	}
	flushCJK := func() {
		if len(cjk) > 0 {
			addRun(cjk)
			cjk = cjk[:0]
		}
	}

	for _, r := range s {
		switch {
		case isCJK(r):
			flushWord()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			word = append(word, r)
		default:
			flushCJK()
			flushWord()
		}
	}
	flushCJK()
	flushWord()
	return out
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}
