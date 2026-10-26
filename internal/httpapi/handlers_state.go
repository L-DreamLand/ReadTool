package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"readtool/internal/kb"
	"readtool/internal/state"
)

// 书签上限：超过就不给加，提示删旧的（设计规格 D-2）。
const maxBookmarks = 500

// 进度上报的最小间隔由前端节流，服务端不再计时：读到哪算到哪，
// 多写几次也只是覆盖同一行。

func (a *App) stateStore() (*state.Store, bool) {
	if a.store == nil {
		return nil, false
	}
	return a.store, true
}

// entryAlive 判断条目"现在还在不在"：目录快照可能比磁盘旧（用户直接删了文件
// 但还没点 ⟳），所以除了查快照还要 stat 一次；最近阅读/书签列表都靠它过滤死链。
func (a *App) entryAlive(path string) (kb.Entry, bool) {
	cat := a.kbPtr.Load()
	e, ok := lookupEntry(cat, path)
	if !ok {
		return kb.Entry{}, false
	}
	if _, _, err := kb.Resolve(cat.KBRoot, path); err != nil {
		return e, false
	}
	return e, true
}

// lookupEntry 把相对路径映射成目录里的条目；不存在返回 nil。
// 进度与书签都只接受"知识库里真实存在的条目"，否则会攒一堆死链（§6.3 实现约束）。
func lookupEntry(cat *kb.Catalog, path string) (kb.Entry, bool) {
	if cat == nil || path == "" {
		return kb.Entry{}, false
	}
	return cat.Entry(path)
}

// ---------- 进度 ----------

type progressResp struct {
	Path      string  `json:"path"`
	ScrollPct float64 `json:"scrollPct"`
	AnchorID  string  `json:"anchorId,omitempty"`
	Device    string  `json:"device,omitempty"`
	UpdatedAt int64   `json:"updatedAt"`
}

func toProgressResp(p state.Progress) progressResp {
	return progressResp{
		Path:      p.Path,
		ScrollPct: p.ScrollPct,
		AnchorID:  p.AnchorID,
		Device:    p.Device,
		UpdatedAt: p.UpdatedAt,
	}
}

// ProgressGet 支持 ?path= 单个 与 ?paths=a,b,c 批量（批量最多 50 个）。
func (a *App) ProgressGet(w http.ResponseWriter, r *http.Request) {
	st, ok := a.stateStore()
	if !ok {
		Error(w, http.StatusServiceUnavailable, CodeServiceUnavail, "状态库未就绪")
		return
	}
	if many := r.URL.Query().Get("paths"); many != "" {
		paths := splitPaths(many, 50)
		items, err := st.ProgressMany(paths)
		if err != nil {
			a.log.Error("读取进度失败", "err", err)
			Error(w, http.StatusInternalServerError, CodeInternal, "读取进度失败")
			return
		}
		out := make([]progressResp, 0, len(items))
		for _, p := range items {
			out = append(out, toProgressResp(p))
		}
		JSON(w, http.StatusOK, map[string]any{"items": out})
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		Error(w, http.StatusBadRequest, CodeBadRequest, "缺少 path 参数")
		return
	}
	p, found, err := st.Progress(path)
	if err != nil {
		a.log.Error("读取进度失败", "err", err)
		Error(w, http.StatusInternalServerError, CodeInternal, "读取进度失败")
		return
	}
	if !found {
		// 没读过不是错误场景，但接口约定用 404 表达，前端据此不弹"续读"（§6.3 12）
		Error(w, http.StatusNotFound, CodeStateNotFound, "没有这个条目的进度")
		return
	}
	JSON(w, http.StatusOK, toProgressResp(p))
}

type progressPutReq struct {
	Path      string  `json:"path"`
	ScrollPct float64 `json:"scrollPct"`
	AnchorID  string  `json:"anchorId"`
	Device    string  `json:"device"`
}

// ProgressPut 写阅读进度（PUT 幂等，同一位置重复写没有副作用）。
func (a *App) ProgressPut(w http.ResponseWriter, r *http.Request) {
	st, ok := a.stateStore()
	if !ok {
		Error(w, http.StatusServiceUnavailable, CodeServiceUnavail, "状态库未就绪")
		return
	}
	var req progressPutReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, CodeBadRequest, "请求体不是合法 JSON")
		return
	}
	cat := a.kbPtr.Load()
	if _, ok := lookupEntry(cat, req.Path); !ok {
		Error(w, http.StatusNotFound, CodePathNotFound, "条目不存在", req.Path)
		return
	}
	if len(req.AnchorID) > 128 {
		req.AnchorID = req.AnchorID[:128]
	}
	if req.Device == "" {
		req.Device = deviceLabel(r)
	}
	if len(req.Device) > 64 {
		req.Device = req.Device[:64]
	}
	pct := req.ScrollPct
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	if err := st.PutProgress(state.Progress{
		Path:      req.Path,
		ScrollPct: pct,
		AnchorID:  req.AnchorID,
		Device:    req.Device,
	}, time.Now().UnixMilli()); err != nil {
		a.log.Error("写入进度失败", "err", err)
		Error(w, http.StatusInternalServerError, CodeInternal, "写入进度失败")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type recentItem struct {
	Path      string  `json:"path"`
	Name      string  `json:"name"`
	Category  string  `json:"category"`
	ScrollPct float64 `json:"scrollPct"`
	UpdatedAt int64   `json:"updatedAt"`
}

// Recent 给首页「最近阅读」（FR-12）：按写入时间倒序，文件已删的条目自动过滤。
func (a *App) Recent(w http.ResponseWriter, r *http.Request) {
	st, ok := a.stateStore()
	if !ok {
		Error(w, http.StatusServiceUnavailable, CodeServiceUnavail, "状态库未就绪")
		return
	}
	limit := 5
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}
	items, err := st.RecentProgress(limit)
	if err != nil {
		a.log.Error("读取最近阅读失败", "err", err)
		Error(w, http.StatusInternalServerError, CodeInternal, "读取最近阅读失败")
		return
	}
	out := make([]recentItem, 0, len(items))
	for _, p := range items {
		e, alive := a.entryAlive(p.Path)
		if !alive {
			continue // 文件已删：不展示死链
		}
		out = append(out, recentItem{
			Path:      p.Path,
			Name:      e.Name,
			Category:  e.Category,
			ScrollPct: p.ScrollPct,
			UpdatedAt: p.UpdatedAt,
		})
	}
	JSON(w, http.StatusOK, map[string]any{"items": out})
}

// ---------- 书签 ----------

type bookmarkResp struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	AnchorID   string `json:"anchorId,omitempty"`
	AnchorText string `json:"anchorText,omitempty"`
	Device     string `json:"device,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
	// Missing 表示目录里已经找不到这个条目（文件被删/改名），前端应标"已失效"
	Missing bool `json:"missing,omitempty"`
}

// BookmarksList 列出全部书签（前端自己在分组/排序上做文章，服务端不做视图逻辑）。
func (a *App) BookmarksList(w http.ResponseWriter, r *http.Request) {
	st, ok := a.stateStore()
	if !ok {
		Error(w, http.StatusServiceUnavailable, CodeServiceUnavail, "状态库未就绪")
		return
	}
	items, err := st.ListBookmarks()
	if err != nil {
		a.log.Error("读取书签失败", "err", err)
		Error(w, http.StatusInternalServerError, CodeInternal, "读取书签失败")
		return
	}
	out := make([]bookmarkResp, 0, len(items))
	for _, b := range items {
		e, alive := a.entryAlive(b.Path)
		resp := bookmarkResp{
			ID:         b.ID,
			Path:       b.Path,
			AnchorID:   b.AnchorID,
			AnchorText: b.AnchorText,
			Device:     b.Device,
			CreatedAt:  b.CreatedAt,
		}
		if alive {
			resp.Name = e.Name
			resp.Category = e.Category
		} else {
			// 条目被删：仍然返回并标 missing，否则用户找不到这条为什么消失
			resp.Missing = true
		}
		out = append(out, resp)
	}
	JSON(w, http.StatusOK, map[string]any{"items": out})
}

type bookmarkCreateReq struct {
	Path       string `json:"path"`
	AnchorID   string `json:"anchorId"`
	AnchorText string `json:"anchorText"`
	Device     string `json:"device"`
}

// BookmarkCreate 加一条书签；返回 id 与创建时间，前端据此就地切换图标状态。
func (a *App) BookmarkCreate(w http.ResponseWriter, r *http.Request) {
	st, ok := a.stateStore()
	if !ok {
		Error(w, http.StatusServiceUnavailable, CodeServiceUnavail, "状态库未就绪")
		return
	}
	var req bookmarkCreateReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, CodeBadRequest, "请求体不是合法 JSON")
		return
	}
	if _, ok := lookupEntry(a.kbPtr.Load(), req.Path); !ok {
		Error(w, http.StatusNotFound, CodePathNotFound, "条目不存在", req.Path)
		return
	}
	if len(req.AnchorID) > 128 {
		req.AnchorID = req.AnchorID[:128]
	}
	if len(req.AnchorText) > 200 {
		req.AnchorText = truncateRunes(req.AnchorText, 200)
	}
	if req.Device == "" {
		req.Device = deviceLabel(r)
	}
	if len(req.Device) > 64 {
		req.Device = req.Device[:64]
	}

	n, err := st.CountBookmarks()
	if err != nil {
		a.log.Error("统计书签失败", "err", err)
		Error(w, http.StatusInternalServerError, CodeInternal, "读取书签失败")
		return
	}
	if n >= maxBookmarks {
		Error(w, http.StatusConflict, CodeStateLimit, "书签数量已达上限，请先删除一些")
		return
	}

	b := state.Bookmark{
		ID:         newID(),
		Path:       req.Path,
		AnchorID:   req.AnchorID,
		AnchorText: req.AnchorText,
		Device:     req.Device,
		CreatedAt:  time.Now().UnixMilli(),
	}
	if err := st.AddBookmark(b); err != nil {
		a.log.Error("写入书签失败", "err", err)
		Error(w, http.StatusInternalServerError, CodeInternal, "写入书签失败")
		return
	}
	JSON(w, http.StatusCreated, map[string]any{"id": b.ID, "createdAt": b.CreatedAt})
}

// BookmarkDelete 删除一条书签。
func (a *App) BookmarkDelete(w http.ResponseWriter, r *http.Request) {
	st, ok := a.stateStore()
	if !ok {
		Error(w, http.StatusServiceUnavailable, CodeServiceUnavail, "状态库未就绪")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		Error(w, http.StatusBadRequest, CodeBadRequest, "缺少书签 id")
		return
	}
	found, err := st.DeleteBookmark(id)
	if err != nil {
		a.log.Error("删除书签失败", "err", err)
		Error(w, http.StatusInternalServerError, CodeInternal, "删除书签失败")
		return
	}
	if !found {
		Error(w, http.StatusNotFound, CodeStateNotFound, "书签不存在", id)
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- 小工具 ----------

// splitPaths 解析 ?paths=a,b,c；顺带做数量与长度上限，避免被拿来扫库。
func splitPaths(raw string, max int) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || len(p) > 512 || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) >= max {
			break
		}
	}
	return out
}

// truncateRunes 按字符（不是字节）截断，中文标题不会被切成半个字。
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// newID 生成 UUIDv4 文本：书签 id 要能安全放进 URL 路径段。
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 失败等于系统随机源坏了，退化成时间戳也比 panic 强
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // 版本 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC4122
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
