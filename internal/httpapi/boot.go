package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
)

// bootConfig 注入到 index.html 的一次性运行参数（设计规格 §5.1）。
// 它替代了"前端再发一轮请求拿配置"，让首屏直接拿到断点阈值与鉴权状态。
type bootConfig struct {
	Version       int64  `json:"version"`
	Dev           bool   `json:"dev"`
	Authenticated bool   `json:"authenticated"`
	NeedSetup     bool   `json:"needSetup"`
	DeviceLabel   string `json:"deviceLabel,omitempty"`

	MinCellPx     int    `json:"minCellPx"`
	CardViewPx    int    `json:"cardViewPx"`
	DrawerPx      int    `json:"drawerPx"`
	DefaultFontPx int    `json:"defaultFontPx"`
	ContentMaxCh  int    `json:"contentMaxCh"`
	DefaultTheme  string `json:"defaultTheme"`
}

// bootToken 是 index.html 里的占位符，**包含两端的引号**。
//
// 为什么要连引号一起替换、且用不同于变量名的 token：
// 若 token 与 JS 变量名相同（都叫 __RT_BOOT__），strings.ReplaceAll 会把
// `window.__RT_BOOT__` 里的变量名一起换掉，产出 `window.{...} = {...}` 这种语法错误。
// 带引号还能保证：万一替换没执行，页面里剩的是一个合法字符串而不是语法错误。
const bootToken = `"__RT_BOOT_JSON__"`

// assetToken 是 index.html 里资源 URL 的指纹占位（`?v=__RT_ASSET_V__`）。
// 替换成当前嵌入资源的哈希，改版后 URL 必然变化，旧缓存不可能被复用。
const assetToken = "__RT_ASSET_V__"

// assetTokenBytes 是 assetToken 的字节形式，serveStatic 每次都拿它扫一遍静态文件。
var assetTokenBytes = []byte(assetToken)

// renderIndex 把 boot 配置填进 index.html。
// Authenticated/NeedSetup 由会话与口令存储决定，前端据此决定是否先跳登录页。
func (a *App) renderIndex(r *http.Request) ([]byte, error) {
	authenticated := false
	label := ""
	if dev, _, ok := a.currentSession(r); ok {
		authenticated = true
		label = deviceLabel(r)
		_ = dev
	}
	boot := bootConfig{
		Version:       a.version.Load(),
		Dev:           a.dev,
		Authenticated: authenticated,
		NeedSetup:     a.needSetup(),
		DeviceLabel:   label,
		MinCellPx:     a.cfg.UI.MinCellPx,
		CardViewPx:    a.cfg.UI.CardViewPx,
		DrawerPx:      a.cfg.UI.DrawerPx,
		DefaultFontPx: a.cfg.UI.DefaultFontPx,
		ContentMaxCh:  a.cfg.UI.ContentMaxCh,
		DefaultTheme:  a.cfg.UI.DefaultTheme,
	}
	raw, err := json.Marshal(boot)
	if err != nil {
		return nil, err
	}
	page := bytes.ReplaceAll(a.indexRaw, []byte(bootToken), raw)
	page = bytes.ReplaceAll(page, []byte(assetToken), []byte(a.assetVersion))
	return page, nil
}

// serveIndex 写出注入了 boot 配置的首页。HTML 一律 no-cache：
// 资源指纹变了但 HTML 被缓存 = 白屏或样式错乱（R-5）。
func (a *App) serveIndex(w http.ResponseWriter, r *http.Request) {
	page, err := a.renderIndex(r)
	if err != nil {
		a.log.Error("render index failed", "err", err)
		Error(w, http.StatusInternalServerError, CodeInternal, "首页渲染失败")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(page)
	}
}
