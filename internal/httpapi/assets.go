package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

// assetVersion 是嵌入前端资源的指纹：把 web/ 下所有文件的内容与路径哈希一遍，
// 取前 8 位十六进制。
//
// 为什么需要它：静态资源如果只写 max-age=3600，用户改版后刷新页面会继续拿到
// 缓存的旧 JS —— 表现为「按钮没出现 / 页面行为和描述不一致」，而且这种问题
// 在服务端看日志一切正常（请求是 200，只是命中了浏览器缓存）。
// 把指纹写进 URL（/src/main.js?v=abc12345）就能让新版本必然是新地址。
func assetVersion(fsys fs.FS) string {
	h := sha256.New()
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil
		}
		// 路径也参与哈希：重命名文件同样应该换指纹
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(b)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:8]
}

// ChromaCSS 提供代码高亮配色（FR-16）。
//
// 内容由 render.ChromaCSS() 在启动时生成一次，指纹取内容 sha256，
// 因此配色变了 URL 也会变，不会出现"改了样式还在用旧缓存"的问题。
func (a *App) ChromaCSS(w http.ResponseWriter, r *http.Request) {
	if len(a.chromaCSS) == 0 {
		http.NotFound(w, r)
		return
	}
	sum := sha256.Sum256(a.chromaCSS)
	etag := `"chroma-` + hex.EncodeToString(sum[:6]) + `"`

	h := w.Header()
	h.Set("Content-Type", "text/css; charset=utf-8")
	if r.URL.Query().Get("v") != "" {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
		h.Set("Etag", etag)
		if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(a.chromaCSS)
}
