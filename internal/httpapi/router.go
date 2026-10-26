package httpapi

import "net/http"

// NewRouter 注册路由。清单见 设计规格 §6.2，本文件是它的一一对应实现。
func NewRouter(app *App) http.Handler {
	mux := http.NewServeMux()

	// 无需登录
	mux.HandleFunc("GET /api/health", app.handleHealth)
	mux.HandleFunc("GET /api/auth/session", app.Session)
	mux.HandleFunc("POST /api/auth/login", app.Login)

	// 需要登录
	mux.HandleFunc("POST /api/auth/logout", app.Logout)
	mux.HandleFunc("POST /api/auth/logout-all", app.LogoutAll)
	mux.HandleFunc("GET /api/kb/meta", app.Meta)
	mux.HandleFunc("GET /api/kb/catalog", app.Catalog)
	mux.HandleFunc("GET /api/kb/doc", app.Doc)
	mux.HandleFunc("GET /api/kb/raw", app.Raw)
	mux.HandleFunc("GET /api/kb/version", app.Version)
	mux.HandleFunc("POST /api/kb/rescan", app.Rescan)
	mux.HandleFunc("GET /api/search", app.Search)
	mux.HandleFunc("GET /assets/chroma.css", app.ChromaCSS)

	// 阅读状态：进度（FR-10/FR-12）与书签（FR-11）
	mux.HandleFunc("GET /api/state/progress", app.ProgressGet)
	mux.HandleFunc("PUT /api/state/progress", app.ProgressPut)
	mux.HandleFunc("GET /api/state/recent", app.Recent)
	mux.HandleFunc("GET /api/state/bookmarks", app.BookmarksList)
	mux.HandleFunc("POST /api/state/bookmarks", app.BookmarkCreate)
	mux.HandleFunc("DELETE /api/state/bookmarks/{id}", app.BookmarkDelete)

	// 未实现的 /api/* 必须返回 JSON 而不是回落到 index.html，
	// 否则前端会把 HTML 当 JSON 解析，报错信息完全对不上真实原因。
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		Error(w, http.StatusNotFound, CodeBadRequest, "接口不存在", r.Method+" "+r.URL.Path)
	})

	mux.HandleFunc("/", app.serveStatic)
	return mux
}
