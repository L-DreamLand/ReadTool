// @ts-check
package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type searchHitResp struct {
	Path     string  `json:"path"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Score    float64 `json:"score"`
	Count    int     `json:"count"`
	Snippet  string  `json:"snippet"`
}

type searchResp struct {
	Query string          `json:"query"`
	Total int             `json:"total"`
	Took  int64           `json:"took"`
	Hits  []searchHitResp `json:"hits"`
}

// 搜索接口（FR-09）：鉴权、参数校验、命中结构、过滤。
func TestSearchEndpoint(t *testing.T) {
	f := newFixture(t)
	// 补一篇带独特词的条目，避免依赖 sampleDoc 的内容
	extra := filepath.Join(f.root, "编程语言", "并发.md")
	if err := os.WriteFile(extra, []byte("# 并发\n\n虚拟线程与 goroutine 的差别。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	// 未登录：401（鉴权中间件先于参数校验）
	if rec := do(f.app, http.MethodGet, "/api/search?q="+escapePath("虚拟线程"), nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应为 401，实际 %d %s", rec.Code, rec.Body.String())
	}

	cookie := login(t, f.app, "search-pass-2026")

	// q 为空 → 400 bad_request
	rec := doJSON(f.app, http.MethodGet, "/api/search?q=", "", cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空 q 应为 400，实际 %d %s", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != CodeBadRequest {
		t.Fatalf("错误码应为 %s，实际 %s", CodeBadRequest, code)
	}

	// 正常命中
	rec = doJSON(f.app, http.MethodGet, "/api/search?q="+escapePath("虚拟线程"), "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("搜索应 200，实际 %d %s", rec.Code, rec.Body.String())
	}
	res := decodeInto[searchResp](t, rec)
	if res.Query != "虚拟线程" || res.Total != 1 || len(res.Hits) != 1 {
		t.Fatalf("命中结构不对：%+v", res)
	}
	hit := res.Hits[0]
	if hit.Path != "编程语言/并发.md" || hit.Category != "编程语言" || hit.Count < 1 || hit.Score <= 0 {
		t.Fatalf("命中字段不对：%+v", hit)
	}
	if !strings.Contains(hit.Snippet, "<em>") {
		t.Fatalf("片段没有高亮：%q", hit.Snippet)
	}

	// 分类过滤
	rec = doJSON(f.app, http.MethodGet, "/api/search?q="+escapePath("虚拟线程")+"&category="+escapePath("其它"), "", cookie)
	res = decodeInto[searchResp](t, rec)
	if res.Total != 0 || len(res.Hits) != 0 {
		t.Fatalf("分类过滤失效：%+v", res.Hits)
	}

	// 仅标题：正文里的词不该命中
	rec = doJSON(f.app, http.MethodGet, "/api/search?q="+escapePath("虚拟线程")+"&title_only=1", "", cookie)
	res = decodeInto[searchResp](t, rec)
	if res.Total != 0 {
		t.Fatalf("仅标题却命中了正文：%+v", res.Hits)
	}

	// 无结果不报错，hits 是空数组而不是 null
	rec = doJSON(f.app, http.MethodGet, "/api/search?q="+escapePath("zzz不存在zzz"), "", cookie)
	res = decodeInto[searchResp](t, rec)
	if res.Total != 0 {
		t.Fatalf("乱码串不该命中：%+v", res.Hits)
	}
	if !strings.Contains(rec.Body.String(), `"hits":[]`) {
		t.Fatalf("空结果时 hits 应为 []，实际 %s", rec.Body.String())
	}
}

// 重扫后索引必须跟着换（FR-09 规则 6 + FR-21）。
func TestSearchIndexFollowsRescan(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "search-pass-2026")

	probe := "独角兽词"

	rec := doJSON(f.app, http.MethodGet, "/api/search?q="+escapePath(probe), "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("搜索失败：%d %s", rec.Code, rec.Body.String())
	}
	if res := decodeInto[searchResp](t, rec); res.Total != 0 {
		t.Fatalf("重扫前不该命中：%+v", res.Hits)
	}

	// 往磁盘里加一篇含该词的文件，然后走 rescan 接口
	added := filepath.Join(f.root, "编程语言", "新篇.md")
	if err := os.WriteFile(added, []byte("# 新篇\n\n这里出现了"+probe+"。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec = doJSON(f.app, http.MethodPost, "/api/kb/rescan", "{}", cookie); rec.Code != http.StatusOK {
		t.Fatalf("rescan 失败：%d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(f.app, http.MethodGet, "/api/search?q="+escapePath(probe), "", cookie)
	res := decodeInto[searchResp](t, rec)
	if res.Total != 1 {
		t.Fatalf("重扫后应能搜到新条目：%+v", res.Hits)
	}
	if res.Hits[0].Path != "编程语言/新篇.md" {
		t.Fatalf("命中的应是新条目，实际 %s", res.Hits[0].Path)
	}
}
