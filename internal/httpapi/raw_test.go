package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FR-17：原文下载。
// 两条硬要求：①默认 inline（分享出去点开是原文）；②download=1 变 attachment。
// 更要紧的是字节一致——所以这里专门造一个带 BOM + CRLF 的文件，
// 任何"顺手 TrimSpace/去 BOM/换行规范化"的改动都会被这条测试抓住。
func TestRawDispositionAndBytes(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "raw-pass-2026")

	// 带 BOM 的内容：BOM(EF BB BF) + CRLF 换行 + 中文标题
	body := append([]byte{0xEF, 0xBB, 0xBF}, []byte("# 带 BOM 的一篇\r\n\r\n正文\r\n")...)
	path := filepath.Join(f.root, "编程语言", "bom.md")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	// 新文件要重扫才对 API 可见
	if _, err := f.app.Rebuild(); err != nil {
		t.Fatalf("重扫失败：%v", err)
	}

	cases := []struct {
		name       string
		query      string
		wantPrefix string
	}{
		{"默认 inline", "?path=" + escapePath("编程语言/bom.md"), "inline;"},
		{"download=1", "?path=" + escapePath("编程语言/bom.md") + "&download=1", "attachment;"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/kb/raw"+c.query, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		f.app.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s：状态码 %d，body=%s", c.name, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
			t.Fatalf("%s：Content-Type=%q", c.name, ct)
		}
		cd := rec.Header().Get("Content-Disposition")
		if !strings.HasPrefix(cd, c.wantPrefix) {
			t.Fatalf("%s：Content-Disposition=%q，期望前缀 %q", c.name, cd, c.wantPrefix)
		}
		// 文件名用 RFC 5987 编码，中文不会出现在裸文本里
		if !strings.Contains(cd, "filename*=UTF-8''") {
			t.Fatalf("%s：缺少 RFC 5987 文件名，得到 %q", c.name, cd)
		}
		if got := rec.Body.Bytes(); string(got) != string(body) {
			t.Fatalf("%s：字节不一致\n got=%q\nwant=%q", c.name, got, body)
		}
	}
}

// 未登录必须 401：原文是笔记正文，不能因为"只是 .md"就漏鉴权。
func TestRawRequiresAuth(t *testing.T) {
	f := newFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/kb/raw?path="+escapePath("编程语言/技能.md"), nil)
	rec := httptest.NewRecorder()
	f.app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，得到 %d", rec.Code)
	}
}
