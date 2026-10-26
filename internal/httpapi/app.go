// Package httpapi 是 HTTP 层：路由、中间件、JSON 响应、静态资源与 boot 注入。
//
// 依赖方向：kb/render/state/auth ← httpapi（设计规格 R2/R4）。
package httpapi

import (
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	assets "readtool"
	"readtool/internal/auth"
	"readtool/internal/config"
	"readtool/internal/kb"
	"readtool/internal/render"
	"readtool/internal/search"
	"readtool/internal/server"
	"readtool/internal/state"
)

// Options 是构造 App 需要的全部外部依赖。nil 字段一律按"未接入"处理，
// 这样骨架阶段（无知识库、无鉴权）也能启动。
type Options struct {
	Cfg    *config.Config
	Log    *slog.Logger
	Listen *server.Manager

	// KB 是启动时扫描到的知识库快照；nil 表示尚未接入。
	KB *kb.Catalog
	// Renderer 负责 markdown → HTML；nil 时文档接口返回 503。
	Renderer *render.Renderer
	// Store 是状态库（密钥、口令哈希）。
	Store *state.Store
	// Session 签发会话 Cookie。
	Session *auth.Session
	// Limiter 限制登录失败次数。
	Limiter *auth.Limiter

	// ScannedAt 是首次扫描完成时间。
	ScannedAt time.Time

	// Search 是启动时建好的搜索索引（FR-09）；nil 时 handler 回 503。
	Search *search.Index

	// Dev 为 true 时静态资源禁用缓存，便于手改 CSS 后直接刷新。
	Dev bool
}

// App 持有一个进程内所有请求共享的只读依赖。
type App struct {
	cfg    *config.Config
	log    *slog.Logger
	listen *server.Manager

	// kbPtr / rendererPtr 是"当前快照"，重扫时整对象替换（设计规格 §4.2）。
	// 用 atomic.Pointer 而不是加锁：请求线程读的是指针本身，替换是原子的。
	kbPtr       atomic.Pointer[kb.Catalog]
	rendererPtr atomic.Pointer[render.Renderer]
	searchPtr   atomic.Pointer[search.Index]
	store       *state.Store
	sess        *auth.Session
	limiter     *auth.Limiter

	startedAt time.Time
	scannedAt atomic.Int64 // 最近一次扫描完成时间（毫秒）
	// 手动重扫的节流与互斥（FR-21）：同一 IP 5 秒内只允许一次，
	// 并且扫描期间持有锁，避免连点两下并发扫两遍。
	rescanMu   sync.Mutex
	rescanLast map[string]time.Time

	// version 是文档版本，前端轮询比对；hot update 会刷新它。
	version atomic.Int64
	// changedAt 是最近一次知识库变化的时间（毫秒）。
	changedAt atomic.Int64
	// hash 是当前口令哈希（"" 表示还没设置过）。
	hash atomic.Value

	// searchReady 已由 searchPtr 取代（FR-09 接入）；保留占位会误导，直接删除。
	dev bool

	staticFS fs.FS
	// assetVersion 是嵌入前端资源的指纹（assets.go），用于 ?v= 缓存击穿
	assetVersion string
	// chromaCSS 是代码高亮配色（FR-16），由 render.ChromaCSS 运行时生成
	chromaCSS []byte
	indexRaw  []byte // index.html 原文，含 __RT_BOOT__ 与资源指纹占位
}

// New 组装 App。嵌入资源缺失属于构建期错误，直接返回 error 由 cmd 退出。
func New(opts Options) (*App, error) {
	if opts.Cfg == nil {
		return nil, errNilConfig
	}
	sub, err := fs.Sub(assets.WebFS, "web")
	if err != nil {
		return nil, err
	}
	indexRaw, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, err
	}
	a := &App{
		cfg:       opts.Cfg,
		log:       defaultLogger(opts.Log),
		listen:    opts.Listen,
		store:     opts.Store,
		sess:      opts.Session,
		limiter:   opts.Limiter,
		startedAt: time.Now(),
		dev:       opts.Dev,
		staticFS:  sub,
		indexRaw:  indexRaw,
	}
	a.rescanLast = make(map[string]time.Time)
	a.assetVersion = assetVersion(sub)
	a.chromaCSS = []byte(render.ChromaCSS())
	a.kbPtr.Store(opts.KB)
	a.rendererPtr.Store(opts.Renderer)
	if opts.Search != nil {
		a.searchPtr.Store(opts.Search)
	} else if opts.KB != nil {
		// 索引纯内存、36 篇约 20ms，启动时同步建好，之后每次重扫一起换（FR-09 规则 6）
		if idx, err := search.Build(opts.KB); err != nil {
			a.log.Warn("搜索索引构建不完整", "err", err)
			a.searchPtr.Store(idx)
		} else {
			a.searchPtr.Store(idx)
		}
	}
	if opts.ScannedAt.IsZero() {
		a.scannedAt.Store(time.Now().UnixMilli())
	} else {
		a.scannedAt.Store(opts.ScannedAt.UnixMilli())
	}
	a.hash.Store("")
	if a.store != nil {
		if h, ok, err := a.store.Secret("password_hash"); err == nil && ok {
			a.hash.Store(h)
		}
	}
	if cat := a.kbPtr.Load(); cat != nil {
		a.version.Store(cat.Version)
	} else {
		a.version.Store(1)
	}
	return a, nil
}

// Cfg 暴露配置给同包 handler 使用。
func (a *App) Cfg() *config.Config { return a.cfg }

// Logger 便于 handler 打点。
func (a *App) Logger() *slog.Logger { return a.log }

// KB 返回当前知识库快照。
func (a *App) KB() *kb.Catalog { return a.kbPtr.Load() }

// Renderer 返回与当前快照配套的渲染器。
// SearchIndex 返回当前搜索索引（可能为 nil）。
func (a *App) SearchIndex() *search.Index { return a.searchPtr.Load() }

func (a *App) Renderer() *render.Renderer { return a.rendererPtr.Load() }

// ScannedAt 返回最近一次扫描完成时间（毫秒）。
func (a *App) ScannedAt() int64 { return a.scannedAt.Load() }

// Handler 返回带完整中间件的 http.Handler。
func (a *App) Handler() http.Handler {
	return Chain(NewRouter(a),
		Recover(a.log),
		RequestID(),
		AccessLog(a.log),
		Gzip(),
		RequireAuth(a),
	)
}

func defaultLogger(l *slog.Logger) *slog.Logger {
	if l != nil {
		return l
	}
	return slog.New(slog.DiscardHandler)
}

// Diff 是重扫后相对上一次快照的变化（FR-21），只用于界面提示。
type Diff struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Changed int `json:"changed"`
}

// Rebuild 重新扫描知识库并原子替换快照。
//
// 三条约束（设计规格 §4.2）：
//   - 扫描在锁外完成，失败时不改动任何现有快照；
//   - 替换顺序必须是 目录 → 渲染器 → 版本，保证任何中间态都自洽；
//   - 渲染器持有 Catalog（标题回退、链接改写、上下篇），必须与目录一起换。
func (a *App) Rebuild() (Diff, error) {
	cat, err := kb.Scan(a.cfg.KB.Root, kb.Options{ShowWorkspace: a.cfg.KB.ShowWorkspace})
	if err != nil {
		return Diff{}, err
	}
	old := a.kbPtr.Load()
	rd := render.New(cat, render.Options{Catalog: cat})

	idx, idxErr := search.Build(cat)
	if idxErr != nil {
		a.log.Warn("搜索索引构建不完整", "err", idxErr)
	}

	a.kbPtr.Store(cat)
	a.rendererPtr.Store(rd)
	a.searchPtr.Store(idx)
	a.version.Store(cat.Version)
	now := time.Now().UnixMilli()
	a.scannedAt.Store(now)
	a.changedAt.Store(now)

	return diffOf(old, cat), nil
}

// diffOf 按 path 对比两次快照：新增、消失、以及名字/大小变化的条目。
func diffOf(old, cur *kb.Catalog) Diff {
	if cur == nil {
		if old == nil {
			return Diff{}
		}
		return Diff{Removed: old.Total}
	}
	if old == nil {
		return Diff{Added: cur.Total}
	}
	type sig struct {
		name string
		size int64
	}
	prev := make(map[string]sig, old.Total)
	for _, p := range old.Paths() {
		if e, ok := old.Entry(p); ok {
			prev[p] = sig{name: e.Name, size: e.Size}
		}
	}
	var d Diff
	for _, p := range cur.Paths() {
		e, ok := cur.Entry(p)
		if !ok {
			continue
		}
		ps, existed := prev[p]
		switch {
		case !existed:
			d.Added++
		case ps.name != e.Name || ps.size != e.Size:
			d.Changed++
		}
		delete(prev, p)
	}
	d.Removed = len(prev)
	return d
}
