package httpapi

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// serveStatic 提供嵌入的前端资源，并为 History 路由兜底（未知路径返回 index.html）。
func (a *App) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		Error(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "静态资源只支持 GET/HEAD")
		return
	}

	// path.Clean("/"+p) 消掉 ../ 与重复斜杠，避免越界读
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || name == "index.html" {
		a.serveIndex(w, r)
		return
	}

	data, err := fs.ReadFile(a.staticFS, name)
	if err != nil {
		// 带扩展名的请求是真资源，缺了就是 404 —— 不能回落到 index.html，
		// 否则浏览器会拿到一段 HTML 当 JS/CSS 执行（控制台报 MIME 错误，很难看出原因）。
		if looksLikeAsset(name) {
			Error(w, http.StatusNotFound, CodePathNotFound, "静态资源不存在")
			return
		}
		// 无扩展名的路径是 History 路由（/doc/xxx、/c/xxx），回落首页交给前端路由
		a.serveIndex(w, r)
		return
	}

	w.Header().Set("Content-Type", contentTypeOf(name))
	// 带正确指纹的 URL 可以长缓存（改版必然换 URL）；没有指纹的请求（ES module
	// 内部 import 的相对路径）必须每次回源校验，否则改版后浏览器还在跑旧代码（R-5）。
	if a.dev {
		w.Header().Set("Cache-Control", "no-store")
	} else if v := r.URL.Query().Get("v"); v != "" && v == a.assetVersion {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
		etag := `"` + a.assetVersion + `-` + name + `"`
		w.Header().Set("ETag", etag)
		// 校验没过就 304：no-cache 也要省掉重复传输
		if inm := r.Header.Get("If-None-Match"); inm != "" && strings.Contains(inm, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	// ES module 内部的 import 也带同一枚指纹。只给入口加版本是不够的：
	// 入口 URL 变了、依赖 URL 没变，浏览器会把新版入口和旧版依赖拼在一起，
	// 链接期报 "does not provide an export named ..."，整张模块图加载失败，
	// 页面永远停在「载入中」，而服务端日志全是 200。
	if bytes.Contains(data, assetTokenBytes) {
		data = bytes.ReplaceAll(data, assetTokenBytes, []byte(a.assetVersion))
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

// looksLikeAsset 判断这个路径是不是「真资源请求」。
//
// 注意：知识库的条目路径本身以 .md 结尾（/doc/编程语言/技能.md），
// 所以不能只看有没有扩展名，必须先按路由前缀排除。
func looksLikeAsset(name string) bool {
	first := name
	if i := strings.IndexByte(name, '/'); i >= 0 {
		first = name[:i]
	}
	switch first {
	case "doc", "c", "api":
		return false // History 路由，回落 index.html
	}

	ext := path.Ext(path.Base(name))
	return ext != "" && ext != "."
}

// contentTypeOf 只覆盖本项目实际用到的扩展名。
// 不用 mime.TypeByExtension：Windows 注册表里的 .js 可能是 text/plain，
// 会让 ES module 被浏览器拒绝执行。
func contentTypeOf(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".webmanifest":
		return "application/manifest+json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}
