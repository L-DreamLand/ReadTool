package httpapi

import (
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"time"

	"readtool/internal/kb"
)

func encodeSecret(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }

func decodeSecret(s string) ([]byte, error) { return base64.RawStdEncoding.DecodeString(s) }

type metaResp struct {
	KBRoot        string `json:"kbRoot"`
	EntryCount    int    `json:"entryCount"`
	CategoryCount int    `json:"categoryCount"`
	LastScanAt    int64  `json:"lastScanAt"`
	LastChangeAt  int64  `json:"lastChangeAt"`
	Version       int64  `json:"version"`
	SearchReady   bool   `json:"searchReady"`
}

// Meta 给前端提供"知识库有哪些东西"的摘要。
func (a *App) Meta(w http.ResponseWriter, r *http.Request) {
	resp := metaResp{
		LastScanAt:   a.scannedAt.Load(),
		LastChangeAt: a.changedAt.Load(),
		Version:      a.version.Load(),
	}
	if cat := a.kbPtr.Load(); cat != nil {
		resp.KBRoot = cat.KBRoot
		resp.EntryCount = cat.Total
		resp.CategoryCount = len(cat.Categories)
	}
	if resp.LastChangeAt == 0 {
		resp.LastChangeAt = resp.LastScanAt
	}
	resp.SearchReady = a.searchPtr.Load() != nil
	JSON(w, http.StatusOK, resp)
}

// Catalog 直接序列化目录树快照。
func (a *App) Catalog(w http.ResponseWriter, r *http.Request) {
	cat := a.kbPtr.Load()
	if cat == nil {
		Error(w, http.StatusServiceUnavailable, CodeServiceUnavail, "知识库尚未就绪")
		return
	}
	JSON(w, http.StatusOK, cat)
}

// Version 只回一个版本号，供前端 10s 轮询（响应体要小于 100 字节）。
func (a *App) Version(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]int64{"version": a.version.Load()})
}

type navItem struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type docResp struct {
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	Title     string    `json:"title"`
	Category  string    `json:"category"`
	HTML      string    `json:"html"`
	TOC       []tocItem `json:"toc"`
	Prev      *navItem  `json:"prev"`
	Next      *navItem  `json:"next"`
	WordCount int       `json:"wordCount"`
	Version   int64     `json:"version"`
}

// tocItem 与 render.TOCItem 同形，这里重新声明是为了让 JSON 字段名成为 API 契约的一部分。
type tocItem struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Level int    `json:"level"`
}

// Doc 渲染一篇条目。
func (a *App) Doc(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if rel == "" {
		Error(w, http.StatusBadRequest, CodeBadRequest, "缺少 path 参数")
		return
	}
	// 取一次快照：重扫可能在中途替换指针，同一请求内必须用同一份。
	cat := a.kbPtr.Load()
	rd := a.rendererPtr.Load()
	if cat == nil || rd == nil {
		Error(w, http.StatusServiceUnavailable, CodeServiceUnavail, "知识库尚未就绪")
		return
	}

	res, err := rd.Render(rel)
	if err != nil {
		switch {
		case errors.Is(err, kb.ErrOutside):
			Error(w, http.StatusForbidden, CodePathOutside, "路径越界")
		case errors.Is(err, kb.ErrNotFound):
			Error(w, http.StatusNotFound, CodePathNotFound, "条目不存在")
		default:
			a.log.Error("渲染失败", "path", rel, "err", err)
			Error(w, http.StatusInternalServerError, CodeInternal, "渲染失败")
		}
		return
	}

	resp := docResp{
		Path:      res.Path,
		Title:     res.Title,
		HTML:      res.HTML,
		TOC:       make([]tocItem, 0, len(res.TOC)),
		WordCount: res.WordCount,
		Version:   res.Version,
	}
	for _, t := range res.TOC {
		resp.TOC = append(resp.TOC, tocItem{ID: t.ID, Text: t.Text, Level: t.Level})
	}
	if e, ok := cat.Entry(res.Path); ok {
		resp.Name = e.Name
		resp.Category = e.Category
	}
	if resp.Name == "" {
		resp.Name = res.Title
	}
	prev, next := cat.Neighbors(res.Path)
	if prev != nil {
		resp.Prev = &navItem{Path: prev.Path, Name: prev.Name}
	}
	if next != nil {
		resp.Next = &navItem{Path: next.Path, Name: next.Name}
	}
	JSON(w, http.StatusOK, resp)
}

// Raw 返回 markdown 原文（"看源码"用），不经过渲染。
func (a *App) Raw(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if rel == "" {
		Error(w, http.StatusBadRequest, CodeBadRequest, "缺少 path 参数")
		return
	}
	cat := a.kbPtr.Load()
	if cat == nil {
		Error(w, http.StatusServiceUnavailable, CodeServiceUnavail, "知识库尚未就绪")
		return
	}
	abs, norm, err := kb.Resolve(cat.KBRoot, rel)
	if err != nil {
		switch {
		case errors.Is(err, kb.ErrOutside):
			Error(w, http.StatusForbidden, CodePathOutside, "路径越界")
		default:
			Error(w, http.StatusNotFound, CodePathNotFound, "条目不存在")
		}
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		Error(w, http.StatusNotFound, CodePathNotFound, "条目不存在")
		return
	}
	name := norm
	if e, ok := cat.Entry(norm); ok {
		name = e.Name
	}
	// 默认 inline：分享出去点开就是原文。
	// download=1 才用 attachment，让浏览器存成 <标题>.md 而不是在标签页里展示。
	disposition := "inline"
	if v := r.URL.Query().Get("download"); v == "1" || v == "true" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", disposition+"; filename*=UTF-8''"+escapeFilename(name+".md"))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

// escapeFilename 做 RFC 5987 编码（文件名里有中文）。
func escapeFilename(name string) string {
	var b []byte
	for _, c := range []byte(name) {
		switch {
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_':
			b = append(b, c)
		default:
			b = append(b, '%')
			b = append(b, "0123456789ABCDEF"[c>>4])
			b = append(b, "0123456789ABCDEF"[c&0x0F])
		}
	}
	return string(b)
}

const rescanMinGap = 5 * time.Second

type rescanResp struct {
	Version    int64 `json:"version"`
	Total      int   `json:"total"`
	Categories int   `json:"categories"`
	Added      int   `json:"added"`
	Removed    int   `json:"removed"`
	Changed    int   `json:"changed"`
	TookMs     int64 `json:"tookMs"`
	LastScanAt int64 `json:"lastScanAt"`
}

// Rescan 手动重新扫描知识库（FR-21）。
//
// 节流按客户端 IP 计算，并且扫描期间持有锁：连点两下不该扫两遍。
// 失败时把该 IP 的时间戳清掉，否则用户要白等 5 秒才能再试。
func (a *App) Rescan(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	now := time.Now()

	a.rescanMu.Lock()
	defer a.rescanMu.Unlock()
	if last, ok := a.rescanLast[ip]; ok && now.Sub(last) < rescanMinGap {
		w.Header().Set("Retry-After", "5")
		Error(w, http.StatusTooManyRequests, CodeRateLimited, "扫描太频繁，请稍后再试")
		return
	}
	a.rescanLast[ip] = now

	start := time.Now()
	diff, err := a.Rebuild()
	took := time.Since(start).Milliseconds()
	if err != nil {
		delete(a.rescanLast, ip)
		a.log.Error("重新扫描失败", "err", err)
		Error(w, http.StatusInternalServerError, CodeScanFailed, "重新扫描失败", err.Error())
		return
	}

	resp := rescanResp{
		Added:      diff.Added,
		Removed:    diff.Removed,
		Changed:    diff.Changed,
		TookMs:     took,
		LastScanAt: a.scannedAt.Load(),
	}
	if cat := a.kbPtr.Load(); cat != nil {
		resp.Version = cat.Version
		resp.Total = cat.Total
		resp.Categories = len(cat.Categories)
	}
	a.log.Info("手动重扫完成", "added", diff.Added, "removed", diff.Removed, "changed", diff.Changed, "tookMs", took)
	JSON(w, http.StatusOK, resp)
}

// nowMillis 便于 handler 计算时间戳。
func nowMillis() int64 { return time.Now().UnixMilli() }
