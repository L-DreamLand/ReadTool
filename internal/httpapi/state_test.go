package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"readtool/internal/state"
)

// escapePath 把相对路径编码进 query：中文与斜杠都必须转义。
func escapePath(p string) string { return url.QueryEscape(p) }

// errorCode 从统一错误体里取 code，避免在断言里写重复的 JSON 解析。
func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("错误响应不是合法 JSON：%v %s", err, rec.Body.String())
	}
	return body.Error.Code
}

// doJSON 发一个带 JSON 体的请求（进度/书签都是写接口，需要真下发 body）。
func doJSON(app *App, method, target, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	return rec
}

func decodeInto[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("响应不是合法 JSON：%v %s", err, rec.Body.String())
	}
	return v
}

// 覆盖 FR-10：写进度、读进度、未记录过返回 404、路径不存在拒绝写入。
func TestProgressPutGet(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "verify-pass-1")
	doc := "编程语言/技能.md"

	rec := doJSON(f.app, http.MethodPut, "/api/state/progress",
		`{"path":"`+doc+`","scrollPct":0.42,"anchorId":"二级标题-a","device":"iPad-Via"}`, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("写进度状态码 %d：%s", rec.Code, rec.Body.String())
	}

	rec = do(f.app, http.MethodGet, "/api/state/progress?path="+escapePath(doc), cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("读进度状态码 %d：%s", rec.Code, rec.Body.String())
	}
	p := decodeInto[progressResp](t, rec)
	if p.Path != doc || p.AnchorID != "二级标题-a" || p.Device != "iPad-Via" {
		t.Fatalf("进度字段不对：%+v", p)
	}
	if p.ScrollPct < 0.419 || p.ScrollPct > 0.421 {
		t.Fatalf("scrollPct 未按原值保存：%v", p.ScrollPct)
	}
	if p.UpdatedAt <= 0 {
		t.Fatal("updatedAt 应该是服务端时间戳")
	}

	// 没读过的条目：404，前端据此不弹「续读」
	rec = do(f.app, http.MethodGet, "/api/state/progress?path="+escapePath("编程语言/other.md"), cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未记录的条目应该 404，实际 %d", rec.Code)
	}
	if code := errorCode(t, rec); code != CodeStateNotFound {
		t.Fatalf("错误码应为 %s，实际 %s", CodeStateNotFound, code)
	}

	// 不存在的路径不能写进库，否则状态库会攒死链
	rec = doJSON(f.app, http.MethodPut, "/api/state/progress", `{"path":"不存在/幽灵.md","scrollPct":0.5}`, cookie)
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != CodePathNotFound {
		t.Fatalf("写入不存在的条目应 404 kb.path_not_found，实际 %d %s", rec.Code, rec.Body.String())
	}

	// 越界的 scrollPct 被 clamp 到 1
	rec = doJSON(f.app, http.MethodPut, "/api/state/progress", `{"path":"`+doc+`","scrollPct":9}`, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("clamp 写入失败 %d", rec.Code)
	}
	rec = do(f.app, http.MethodGet, "/api/state/progress?path="+escapePath(doc), cookie)
	if got := decodeInto[progressResp](t, rec).ScrollPct; got != 1 {
		t.Fatalf("scrollPct 应被夹到 1，实际 %v", got)
	}

	// 未登录一律 401
	if rec := do(f.app, http.MethodGet, "/api/state/progress?path="+escapePath(doc), nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，实际 %d", rec.Code)
	}
}

// 覆盖 FR-12：批量进度 + 最近阅读按时间倒序、已删除的条目自动过滤。
func TestRecentAndProgressMany(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "verify-pass-2")
	a, b := "编程语言/技能.md", "编程语言/other.md"

	for _, p := range []string{a, b} {
		if rec := doJSON(f.app, http.MethodPut, "/api/state/progress",
			`{"path":"`+p+`","scrollPct":0.5}`, cookie); rec.Code != http.StatusOK {
			t.Fatalf("写进度失败 %s：%d", p, rec.Code)
		}
	}

	joined := url.QueryEscape(a + "," + b + ",不存在/x.md")
	rec := do(f.app, http.MethodGet, "/api/state/progress?paths="+joined, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("批量读状态码 %d", rec.Code)
	}
	many := decodeInto[struct {
		Items []progressResp `json:"items"`
	}](t, rec)
	if len(many.Items) != 2 {
		t.Fatalf("批量读应返回 2 条（缺失的不报错），实际 %d", len(many.Items))
	}

	rec = do(f.app, http.MethodGet, "/api/state/recent?limit=5", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("最近阅读状态码 %d：%s", rec.Code, rec.Body.String())
	}
	type recentBody struct {
		Items []recentItem `json:"items"`
	}
	body := decodeInto[recentBody](t, rec)
	if len(body.Items) != 2 {
		t.Fatalf("最近阅读应有 2 条，实际 %d", len(body.Items))
	}
	if body.Items[0].UpdatedAt < body.Items[1].UpdatedAt {
		t.Fatal("最近阅读必须按 updatedAt 倒序")
	}
	if body.Items[0].Name == "" || body.Items[0].Category == "" {
		t.Fatalf("最近阅读要带上条目名与分类：%+v", body.Items[0])
	}

	// 文件被删后不能再出现在最近阅读里（否则首页全是死链）
	if err := os.Remove(filepath.Join(f.root, filepath.FromSlash(b))); err != nil {
		t.Fatal(err)
	}
	rec = do(f.app, http.MethodGet, "/api/state/recent", cookie)
	body = decodeInto[recentBody](t, rec)
	for _, it := range body.Items {
		if it.Path == b {
			t.Fatalf("已删除的条目不应出现在最近阅读：%+v", it)
		}
	}
}

// 覆盖 FR-11：书签增删查、条目级与锚点级、列表带名字与分类。
func TestBookmarksCRUD(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "verify-pass-3")
	doc := "编程语言/技能.md"

	// 锚点级
	rec := doJSON(f.app, http.MethodPost, "/api/state/bookmarks",
		`{"path":"`+doc+`","anchorId":"二级标题-b","anchorText":"二级标题 B"}`, cookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("加书签状态码 %d：%s", rec.Code, rec.Body.String())
	}
	created := decodeInto[struct {
		ID        string `json:"id"`
		CreatedAt int64  `json:"createdAt"`
	}](t, rec)
	if len(created.ID) != 36 || created.CreatedAt <= 0 {
		t.Fatalf("书签应返回 uuid 与时间戳：%+v", created)
	}

	// 条目级
	rec = doJSON(f.app, http.MethodPost, "/api/state/bookmarks", `{"path":"`+doc+`"}`, cookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("加条目级书签失败 %d", rec.Code)
	}

	rec = do(f.app, http.MethodGet, "/api/state/bookmarks", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("书签列表状态码 %d", rec.Code)
	}
	type listBody struct {
		Items []bookmarkResp `json:"items"`
	}
	list := decodeInto[listBody](t, rec)
	if len(list.Items) != 2 {
		t.Fatalf("应有 2 条书签，实际 %d", len(list.Items))
	}
	var anchored *bookmarkResp
	for i := range list.Items {
		if list.Items[i].AnchorID != "" {
			anchored = &list.Items[i]
		}
	}
	if anchored == nil {
		t.Fatal("锚点级书签丢了 anchorId")
	}
	if anchored.Name == "" || anchored.Category == "" || anchored.AnchorText != "二级标题 B" {
		t.Fatalf("书签列表要带条目名/分类/锚点文本：%+v", anchored)
	}

	// 删除
	rec = do(f.app, http.MethodDelete, "/api/state/bookmarks/"+created.ID, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("删除书签失败 %d：%s", rec.Code, rec.Body.String())
	}
	// 再删一次：404 state.not_found
	rec = do(f.app, http.MethodDelete, "/api/state/bookmarks/"+created.ID, cookie)
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != CodeStateNotFound {
		t.Fatalf("重复删除应 404 state.not_found，实际 %d %s", rec.Code, rec.Body.String())
	}

	// 路径不存在 → 404
	rec = doJSON(f.app, http.MethodPost, "/api/state/bookmarks", `{"path":"不存在/幽灵.md"}`, cookie)
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != CodePathNotFound {
		t.Fatalf("给不存在的条目加书签应 404，实际 %d", rec.Code)
	}

	// 未登录 401
	if rec := doJSON(f.app, http.MethodPost, "/api/state/bookmarks", `{"path":"`+doc+`"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录加书签应 401，实际 %d", rec.Code)
	}
}

// 书签上限 500：到顶后返回 409 state.limit_exceeded。
func TestBookmarkLimit(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "verify-pass-4")
	doc := "编程语言/技能.md"

	for i := 0; i < maxBookmarks; i++ {
		st, ok := f.app.stateStore()
		if !ok {
			t.Fatal("状态库未接线")
		}
		if err := st.AddBookmark(state.Bookmark{
			ID:        newID(),
			Path:      doc,
			CreatedAt: int64(i + 1),
		}); err != nil {
			t.Fatalf("批量插入书签失败：%v", err)
		}
	}

	rec := doJSON(f.app, http.MethodPost, "/api/state/bookmarks", `{"path":"`+doc+`"}`, cookie)
	if rec.Code != http.StatusConflict {
		t.Fatalf("到上限应 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != CodeStateLimit {
		t.Fatalf("错误码应为 %s，实际 %s", CodeStateLimit, code)
	}
}
