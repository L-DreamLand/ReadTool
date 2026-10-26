package httpapi

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Middleware 统一形态（设计规格 §6.5）。
type Middleware func(http.Handler) http.Handler

// Chain 把中间件套在 h 上；第一个参数是最外层。
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

type ctxKey int

const ctxKeyRequestID ctxKey = 0

// RequestID 给每个请求分配短 ID，写进日志与 500 响应的 detail，
// 让"平板上报错了"能对上日志里的一行。
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if id == "" || len(id) > 64 {
				var b [8]byte
				_, _ = rand.Read(b[:])
				id = hex.EncodeToString(b[:])
			}
			w.Header().Set("X-Request-Id", id)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, id)))
		})
	}
}

// RequestIDFrom 取出当前请求的 ID（可能为空）。
func RequestIDFrom(r *http.Request) string {
	if v, ok := r.Context().Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

// Recover 兜住 handler panic：返回 500 + requestID，进程继续服务。
func Recover(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					id := RequestIDFrom(r)
					log.Error("panic recovered", "panic", v, "path", r.URL.Path, "request_id", id)
					Error(w, http.StatusInternalServerError, CodeInternal,
						"服务内部错误，请把请求 ID 告知维护者", id)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder 记录状态码与字节数，供访问日志使用。
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// AccessLog 每个请求一行。健康检查走 debug，避免每 5 秒一条噪音。
func AccessLog(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)

			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			level := slog.LevelInfo
			if r.URL.Path == "/api/health" {
				level = slog.LevelDebug
			}
			log.Log(r.Context(), level, "http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFrom(r),
			)
		})
	}
}

// Gzip 只在客户端明确接受且内容类型可压缩时压缩。
// 注意：一旦决定压缩就必须删掉 Content-Length，否则浏览器会截断。
func Gzip() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !acceptsGzip(r) {
				next.ServeHTTP(w, r)
				return
			}
			gw := &gzipWriter{ResponseWriter: w}
			defer gw.close()
			next.ServeHTTP(gw, r)
		})
	}
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.EqualFold(strings.TrimSpace(strings.Split(part, ";")[0]), "gzip") {
			return true
		}
	}
	return false
}

type gzipWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	decided     bool
	compress    bool
	wroteHeader bool
}

func (g *gzipWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	g.decide()
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	if g.compress {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipWriter) decide() {
	if g.decided {
		return
	}
	g.decided = true
	if g.Header().Get("Content-Encoding") != "" {
		return
	}
	if !compressible(g.Header().Get("Content-Type")) {
		return
	}
	g.compress = true
	g.Header().Set("Content-Encoding", "gzip")
	g.Header().Del("Content-Length")
	g.gz = gzip.NewWriter(g.ResponseWriter)
}

func (g *gzipWriter) close() {
	if g.gz != nil {
		_ = g.gz.Close()
	}
}

// Flush 让 SSE/handler 主动刷新时仍然工作（FR-14 用得到）。
func (g *gzipWriter) Flush() {
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func compressible(contentType string) bool {
	ct := strings.ToLower(contentType)
	if ct == "" {
		return false
	}
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	for _, p := range []string{"application/json", "application/javascript", "image/svg+xml", "application/manifest+json"} {
		if strings.HasPrefix(ct, p) {
			return true
		}
	}
	return false
}
