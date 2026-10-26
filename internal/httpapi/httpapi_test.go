package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"readtool/internal/auth"
	"readtool/internal/config"
	"readtool/internal/kb"
	"readtool/internal/render"
	"readtool/internal/state"
)

// 一篇带标题、表格、站内链接的样例笔记，覆盖渲染管线的关键分支。
const sampleDoc = `# 技能总览

这是引言段落，包含一个[兄弟链接](other.md)和一个[坏链接](missing.md)。

## 二级标题 A

| 名称 | 说明 |
| --- | --- |
| 甲 | 第一 |
| 乙 | 第二 |

### 三级标题 A1

正文内容。

## 二级标题 B

| a | b | c | d |
| --- | --- | --- | --- |
| 1 | 2 | 3 | 4 |
`

type fixture struct {
	app  *App
	root string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	cat := filepath.Join(root, "编程语言")
	if err := os.MkdirAll(cat, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cat, "技能.md"), []byte(sampleDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cat, "other.md"), []byte("# 另一篇\n\n内容\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "INDEX.md"), []byte("# 索引\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, err := kb.Scan(root, kb.Options{})
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	store, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("状态库失败：%v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.Default()
	cfg.KB.Root = root
	cfg.Normalize()

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	secret, _ := auth.RandomSecret()
	app, err := New(Options{
		Cfg:      cfg,
		Log:      logger,
		KB:       catalog,
		Renderer: render.New(catalog, render.Options{}),
		Store:    store,
		Session:  &auth.Session{Secret: secret, Days: 30},
		Limiter:  auth.NewLimiter(5, 0, 0),
	})
	if err != nil {
		t.Fatalf("构造 App 失败：%v", err)
	}
	return &fixture{app: app, root: root}
}

// login 走一遍"首次设置口令 + 登录"，返回可用的 Cookie。
func login(t *testing.T, app *App, password string) *http.Cookie {
	t.Helper()
	body := strings.NewReader(`{"password":"` + password + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("登录失败：%d %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	t.Fatal("登录响应没有下发会话 Cookie")
	return nil
}

func do(app *App, method, target string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	return rec
}

// 覆盖 T0.3/T0.4：静态资源能出、boot 占位符必须被替换干净。
func TestIndexInjectsBootConfig(t *testing.T) {
	f := newFixture(t)
	rec := do(f.app, http.MethodGet, "/", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("首页状态码 %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "__RT_BOOT_JSON__") {
		t.Fatal("占位符未被替换，页面会拿到字符串而不是配置对象")
	}
	if !strings.Contains(body, `window.__RT_BOOT__ = {"version":`) {
		t.Fatalf("boot 配置未注入：\n%s", firstLines(body, 3))
	}
	if !strings.Contains(body, `"needSetup":true`) {
		t.Fatal("首次运行 boot 应告知 needSetup=true，否则前端不会引导设口令")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type 应为 text/html，实际 %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("HTML 必须 no-cache（R-5），实际 %q", cc)
	}
}

func TestHealthEndpoint(t *testing.T) {
	f := newFixture(t)
	rec := do(f.app, http.MethodGet, "/api/health", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	body := rec.Body.String()
	// 两个分类条目 + 1 个根目录文件，health 只数分类条目
	for _, want := range []string{`"status":"ok"`, `"entryCount":2`, `"listeners":[]`} {
		if !strings.Contains(body, want) {
			t.Errorf("health 响应缺少 %s：%s", want, body)
		}
	}
}

// 未登录访问业务接口必须 401 且是 JSON（T1.5 Verdict）。
func TestUnauthenticatedAPIBlocked(t *testing.T) {
	f := newFixture(t)
	for _, p := range []string{"/api/whatever", "/api/kb/catalog", "/api/kb/doc?path=x.md"} {
		rec := do(f.app, http.MethodGet, p, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s 未登录应 401，实际 %d", p, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"code":"unauthorized"`) {
			t.Fatalf("%s 错误码不符：%s", p, rec.Body.String())
		}
	}
}

// 登录后未知接口才回 404：不能让"接口不存在"被鉴权错误盖住。
func TestUnknownAPIReturnsJSONAfterLogin(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "testing-password")

	rec := do(f.app, http.MethodGet, "/api/whatever", cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未知接口应 404，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"bad_request"`) {
		t.Fatalf("错误体格式不符：%s", rec.Body.String())
	}
}

// 静态资源存在时按扩展名给 Content-Type；ES module 必须是 text/javascript。
func TestStaticAssetContentType(t *testing.T) {
	f := newFixture(t)
	rec := do(f.app, http.MethodGet, "/src/main.js", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("JS 的 Content-Type 应为 text/javascript，实际 %q", ct)
	}
}

// 未知路径回落到 index.html（History 路由），不能被当成 404。
func TestUnknownPathFallsBackToIndex(t *testing.T) {
	f := newFixture(t)
	rec := do(f.app, http.MethodGet, "/doc/编程语言/技能.md", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("未知前端路径应回落 index，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<!doctype html>") {
		t.Fatal("回落内容不是首页")
	}
}

func TestMissingAssetIs404NotIndex(t *testing.T) {
	f := newFixture(t)
	// 真资源缺失必须 404：返回 index.html 会让浏览器把 HTML 当 JS 执行
	for _, path := range []string{"/src/nope.js", "/styles/missing.css", "/icons/none.svg"} {
		rec := do(f.app, http.MethodGet, path, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s 应 404，实际 %d", path, rec.Code)
		}
	}
	// 条目路径以 .md 结尾，仍要回落 index（前端路由）
	rec := do(f.app, http.MethodGet, "/doc/编程语言/技能.md", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("/doc/*.md 应回落 index，实际 %d", rec.Code)
	}
}

func TestCatalogShape(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "testing-password")

	rec := do(f.app, http.MethodGet, "/api/kb/catalog", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d：%s", rec.Code, rec.Body.String())
	}
	var got struct {
		KBRoot     string `json:"kbRoot"`
		Total      int    `json:"total"`
		Categories []struct {
			Name    string `json:"name"`
			Count   int    `json:"count"`
			Entries []struct {
				Path string `json:"path"`
				Name string `json:"name"`
			} `json:"entries"`
		} `json:"categories"`
		RootFiles []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"rootFiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON：%v", err)
	}
	if got.Total != 2 || len(got.Categories) != 1 {
		t.Fatalf("目录结构不符：total=%d categories=%d", got.Total, len(got.Categories))
	}
	if got.Categories[0].Name != "编程语言" || got.Categories[0].Count != 2 {
		t.Fatalf("分类不符：%+v", got.Categories[0])
	}
	if len(got.RootFiles) != 1 || got.RootFiles[0].Kind != "index" {
		t.Fatalf("根目录文件应识别为 index：%+v", got.RootFiles)
	}
}

func TestDocRendersHTMLAndTOC(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "testing-password")

	rec := do(f.app, http.MethodGet, "/api/kb/doc?path="+pathEscape("编程语言/技能.md"), cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d：%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Path     string `json:"path"`
		Name     string `json:"name"`
		Title    string `json:"title"`
		Category string `json:"category"`
		HTML     string `json:"html"`
		TOC      []struct {
			ID    string `json:"id"`
			Text  string `json:"text"`
			Level int    `json:"level"`
		} `json:"toc"`
		Prev any `json:"prev"`
		Next any `json:"next"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON：%v", err)
	}
	if got.Title != "技能总览" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Category != "编程语言" {
		t.Errorf("category = %q", got.Category)
	}
	if len(got.TOC) != 3 {
		t.Fatalf("TOC 应有 3 条（2 个二级 + 1 个三级），实际 %d：%+v", len(got.TOC), got.TOC)
	}
	if !strings.Contains(got.HTML, `<div class="table-wrap" data-cols="2">`) {
		t.Errorf("2 列表格未按 2 列包装：%s", got.HTML)
	}
	if !strings.Contains(got.HTML, `<div class="table-wrap" data-cols="4">`) {
		t.Errorf("4 列表格未按 4 列包装：%s", got.HTML)
	}
	// 站内链接改写为前端路由
	if !strings.Contains(got.HTML, `href="/doc/%E7%BC%96%E7%A8%8B%E8%AF%AD%E8%A8%80/other.md"`) {
		t.Errorf("兄弟链接未改写为 /doc/ 路由：%s", got.HTML)
	}
	// 不存在的目标要被标记，而不是被丢掉
	if !strings.Contains(got.HTML, `data-broken="1"`) {
		t.Errorf("坏链接未被标记：%s", got.HTML)
	}
}

func TestDocPathOutside(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "testing-password")

	rec := do(f.app, http.MethodGet, "/api/kb/doc?path="+pathEscape("../../etc/passwd"), cookie)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("越界路径应 403，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"kb.path_outside"`) {
		t.Fatalf("错误码不符：%s", rec.Body.String())
	}
}

func TestDocNotFound(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "testing-password")

	rec := do(f.app, http.MethodGet, "/api/kb/doc?path="+pathEscape("编程语言/没有这篇.md"), cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在的条目应 404，实际 %d", rec.Code)
	}
}

func TestLogoutAllInvalidatesCookie(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "testing-password")

	rec := do(f.app, http.MethodPost, "/api/auth/logout-all", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout-all 状态码 %d：%s", rec.Code, rec.Body.String())
	}

	rec = do(f.app, http.MethodGet, "/api/kb/catalog", cookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("轮换密钥后旧 Cookie 必须立即失效，实际 %d", rec.Code)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	f := newFixture(t)
	login(t, f.app, "correct-password")

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"password":"wrong"}`))
	rec := httptest.NewRecorder()
	f.app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("错口令应 401，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"auth.bad_password"`) {
		t.Fatalf("错误码不符：%s", rec.Body.String())
	}
}

func pathEscape(p string) string {
	var b strings.Builder
	for _, c := range []byte(p) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_' || c == '~' || c == '/' {
			b.WriteByte(c)
			continue
		}
		b.WriteString("%")
		b.WriteString(string("0123456789ABCDEF"[c>>4]))
		b.WriteString(string("0123456789ABCDEF"[c&0x0F]))
	}
	return b.String()
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// 资源指纹：index.html 里每个本地资源都必须带 ?v=<8 位指纹>，
// 否则改版后浏览器会继续用缓存里的旧 JS（用户看到的就是「按钮没出现」）。
func TestIndexCarriesAssetFingerprint(t *testing.T) {
	f := newFixture(t)
	rec := do(f.app, http.MethodGet, "/", nil)
	body := rec.Body.String()

	if strings.Contains(body, assetToken) {
		t.Fatal("资源指纹占位符没被替换")
	}
	if !regexp.MustCompile(`/src/main\.js\?v=[0-9a-f]{8}`).MatchString(body) {
		t.Fatalf("main.js 没有带指纹：\n%s", firstLines(body, 20))
	}
	if !regexp.MustCompile(`/src/styles/app\.css\?v=[0-9a-f]{8}`).MatchString(body) {
		t.Fatal("app.css 没有带指纹")
	}
}

// 带正确指纹的资源长缓存；不带指纹的（模块内部 import）必须回源校验。
func TestAssetCachePolicy(t *testing.T) {
	f := newFixture(t)
	stale := do(f.app, http.MethodGet, "/src/main.js", nil)
	if got := stale.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("无指纹请求的 Cache-Control = %q，期望 no-cache", got)
	}
	etag := stale.Header().Get("ETag")
	if etag == "" {
		t.Fatal("无指纹请求缺少 ETag")
	}

	req := httptest.NewRequest(http.MethodGet, "/src/main.js", nil)
	req.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	f.app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("命中 ETag 的状态码 = %d，期望 304", rec.Code)
	}

	fresh := do(f.app, http.MethodGet, "/src/main.js?v="+f.app.assetVersion, nil)
	if got := fresh.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("带指纹请求的 Cache-Control = %q，期望 immutable", got)
	}
}

// 模块内部 import 也必须带指纹：只给入口加版本，浏览器会把新版入口和旧版依赖拼在
// 一起，链接期报 "does not provide an export named ..."，页面永远停在「载入中」。
func TestModuleImportsCarryFingerprint(t *testing.T) {
	f := newFixture(t)
	rec := do(f.app, http.MethodGet, "/src/main.js?v="+f.app.assetVersion, nil)
	body := rec.Body.String()

	if strings.Contains(body, assetToken) {
		t.Fatal("main.js 里还残留指纹占位符")
	}
	if strings.Contains(body, `from "./`) || strings.Contains(body, `from "../`) {
		t.Fatalf("main.js 里还有相对 import（改版后会命中旧依赖）：\n%s", firstLines(body, 12))
	}
	want := "/src/router.js?v=" + f.app.assetVersion
	if !strings.Contains(body, want) {
		t.Fatalf("main.js 的依赖没带指纹，期望包含 %q：\n%s", want, firstLines(body, 12))
	}

	// 入口文件本身也要能取到同一份内容（无 v 参数时走 no-cache + ETag 路径）
	plain := do(f.app, http.MethodGet, "/src/main.js", nil).Body.String()
	if strings.Contains(plain, assetToken) {
		t.Fatal("不带 v 的 main.js 里还残留指纹占位符")
	}
}
