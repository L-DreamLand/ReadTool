package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// 覆盖 FR-21：新增文件后手动重扫能把新条目收进目录。
func TestRescanPicksUpNewFile(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "correct horse battery")

	before := f.app.KB().Total
	if err := os.WriteFile(filepath.Join(f.root, "编程语言", "新增.md"), []byte("# 新篇\n\n正文\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := do(f.app, http.MethodPost, "/api/kb/rescan", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("重扫状态码 %d：%s", rec.Code, rec.Body.String())
	}
	var resp rescanResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败：%v", err)
	}
	if resp.Added != 1 || resp.Removed != 0 || resp.Changed != 0 {
		t.Fatalf("差异不对：%+v", resp)
	}
	if resp.Total != before+1 {
		t.Fatalf("条目数 %d，期望 %d", resp.Total, before+1)
	}
	if got := f.app.KB().Total; got != before+1 {
		t.Fatalf("快照没有替换：%d", got)
	}

	// 目录接口必须立刻反映新条目（前端刷新走的就是它）。
	cat := do(f.app, http.MethodGet, "/api/kb/catalog", cookie)
	if cat.Code != http.StatusOK {
		t.Fatalf("catalog 状态码 %d", cat.Code)
	}
}

// 5 秒内第二次点击必须被拒，并且给出 Retry-After。
func TestRescanThrottled(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "correct horse battery")

	if rec := do(f.app, http.MethodPost, "/api/kb/rescan", cookie); rec.Code != http.StatusOK {
		t.Fatalf("首次重扫状态码 %d：%s", rec.Code, rec.Body.String())
	}
	rec := do(f.app, http.MethodPost, "/api/kb/rescan", cookie)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("第二次重扫状态码 %d，期望 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("缺少 Retry-After")
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析错误体失败：%v", err)
	}
	if body.Error.Code != CodeRateLimited {
		t.Fatalf("错误码 %q，期望 %q", body.Error.Code, CodeRateLimited)
	}
}

// 未登录不能触发扫描（扫目录是会碰磁盘的操作，不该对匿名请求开放）。
func TestRescanRequiresAuth(t *testing.T) {
	f := newFixture(t)
	rec := do(f.app, http.MethodPost, "/api/kb/rescan", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 %d，期望 401", rec.Code)
	}
}

// 扫描失败时：返回 500、错误码 kb.scan_failed，且旧快照仍然可用。
func TestRescanFailureKeepsOldSnapshot(t *testing.T) {
	f := newFixture(t)
	cookie := login(t, f.app, "correct horse battery")
	before := f.app.KB().Total

	if err := os.RemoveAll(f.root); err != nil {
		t.Fatal(err)
	}

	rec := do(f.app, http.MethodPost, "/api/kb/rescan", cookie)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 %d，期望 500：%s", rec.Code, rec.Body.String())
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析错误体失败：%v", err)
	}
	if body.Error.Code != CodeScanFailed {
		t.Fatalf("错误码 %q，期望 %q", body.Error.Code, CodeScanFailed)
	}
	if got := f.app.KB().Total; got != before {
		t.Fatalf("失败不应改动快照：%d → %d", before, got)
	}
}
