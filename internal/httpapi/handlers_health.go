package httpapi

import (
	"net/http"
	"time"

	"readtool/internal/server"
)

// HealthResponse 对应 `GET /api/health` 的响应体（设计规格 §6.3 第 1 条）。
type HealthResponse struct {
	Status     string          `json:"status"`
	KBRoot     string          `json:"kbRoot"`
	EntryCount int             `json:"entryCount"`
	Uptime     int64           `json:"uptime"`
	Version    int64           `json:"version"`
	Listeners  []server.Status `json:"listeners"`
}

// Health 计算当前健康状态。degraded 判定写死（设计规格 §6.3）：
// 任一 listener.ok == false，或搜索快照不可用。
func (a *App) Health() HealthResponse {
	resp := HealthResponse{
		Status:    "ok",
		KBRoot:    a.cfg.KB.Root,
		Uptime:    int64(time.Since(a.startedAt).Seconds()),
		Version:   a.version.Load(),
		Listeners: []server.Status{},
	}
	if cat := a.kbPtr.Load(); cat != nil {
		resp.KBRoot = cat.KBRoot
		resp.EntryCount = cat.Total
	}
	if a.listen != nil {
		resp.Listeners = a.listen.Status()
		if a.listen.AnyFailed() {
			resp.Status = "degraded"
		}
	}
	if a.searchPtr.Load() == nil {
		resp.Status = "degraded"
	}
	return resp
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, a.Health())
}
