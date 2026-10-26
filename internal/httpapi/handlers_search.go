// @ts-check
package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"readtool/internal/search"
)

// Search 全文搜索（FR-09）。
//
// 参数：q（1–64 字符，必填）、category、title_only、limit。
// 索引是内存快照，查询只读，不加锁。
func (a *App) Search(w http.ResponseWriter, r *http.Request) {
	idx := a.searchPtr.Load()
	if idx == nil {
		Error(w, http.StatusServiceUnavailable, CodeSearchUnavail, "搜索索引还没准备好，请稍后再试", "")
		return
	}

	raw := strings.TrimSpace(r.URL.Query().Get("q"))
	if raw == "" {
		Error(w, http.StatusBadRequest, CodeBadRequest, "请先输入要搜索的内容", "q 为空")
		return
	}
	if n := len([]rune(raw)); n > 64 {
		Error(w, http.StatusBadRequest, CodeBadRequest, "搜索词太长了", "q 超过 64 字符")
		return
	}

	titleOnly := r.URL.Query().Get("title_only") == "1" || r.URL.Query().Get("title_only") == "true"

	limit := 20
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}

	res := idx.Search(search.Query{
		Text:      raw,
		Category:  strings.TrimSpace(r.URL.Query().Get("category")),
		TitleOnly: titleOnly,
		Limit:     limit,
	})
	if res.Hits == nil {
		res.Hits = []search.Hit{}
	}
	NoStore(w)
	JSON(w, http.StatusOK, res)
}
