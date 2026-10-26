package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"readtool/internal/auth"
)

const (
	secretSessionKey = "session_secret"
	secretPassword   = "password_hash"
)

// needSetup 表示库里还没有口令哈希（首次运行）。
func (a *App) needSetup() bool {
	return a.currentHash() == ""
}

func (a *App) currentHash() string {
	if v, ok := a.hash.Load().(string); ok {
		return v
	}
	return ""
}

// deviceLabel 由 User-Agent 生成，只用于"这是哪台设备"的提示，不做安全判断。
func deviceLabel(r *http.Request) string {
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	var os, br string
	switch {
	case strings.Contains(ua, "iphone"):
		os = "iPhone"
	case strings.Contains(ua, "ipad"):
		os = "iPad"
	case strings.Contains(ua, "android"):
		os = "Android"
	case strings.Contains(ua, "windows"):
		os = "Windows"
	case strings.Contains(ua, "mac os"):
		os = "Mac"
	case strings.Contains(ua, "linux"):
		os = "Linux"
	}
	switch {
	case strings.Contains(ua, "edg/"):
		br = "Edge"
	case strings.Contains(ua, "chrome/"), strings.Contains(ua, "crios/"):
		br = "Chrome"
	case strings.Contains(ua, "firefox/"), strings.Contains(ua, "fxios/"):
		br = "Firefox"
	case strings.Contains(ua, "safari/"):
		br = "Safari"
	}
	switch {
	case os != "" && br != "":
		return os + "-" + br
	case os != "":
		return os
	case br != "":
		return br
	}
	return "未知设备"
}

// deviceID 是 UA + IP 的短哈希，写进会话载荷；换设备登录不会互相顶掉。
func deviceID(r *http.Request) string {
	sum := sha256.Sum256([]byte(r.UserAgent() + "|" + clientIP(r)))
	return hex.EncodeToString(sum[:8])
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// currentSession 解析 Cookie。
func (a *App) currentSession(r *http.Request) (device string, expiresAt int64, ok bool) {
	if a.sess == nil {
		return "", 0, false
	}
	c, err := r.Cookie(auth.CookieName)
	if err != nil {
		return "", 0, false
	}
	dev, exp, err := a.sess.Parse(c.Value, time.Now())
	if err != nil {
		return "", 0, false
	}
	return dev, exp, true
}

// RequireAuth 拦截除白名单外的一切 /api/ 请求。
//
// 白名单：健康检查、会话查询、登录（这些接口本身要能在未登录时访问），
// 以及静态资源（登录页、CSS、JS 必须能加载，否则进不去）。
func RequireAuth(a *App) Middleware {
	open := map[string]bool{
		"/api/health":       true,
		"/api/auth/session": true,
		"/api/auth/login":   true,
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := r.URL.Path
			if !strings.HasPrefix(p, "/api/") || open[p] {
				next.ServeHTTP(w, r)
				return
			}
			if _, _, ok := a.currentSession(r); !ok {
				NoStore(w)
				Error(w, http.StatusUnauthorized, CodeUnauthorized, "请先登录")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type sessionResp struct {
	Authenticated bool   `json:"authenticated"`
	NeedSetup     bool   `json:"needSetup"`
	DeviceLabel   string `json:"deviceLabel,omitempty"`
	ExpiresAt     int64  `json:"expiresAt,omitempty"`
}

// Session 永远返回 200：未登录也是一种正常状态，登录页靠它决定显示"设口令"还是"输口令"。
func (a *App) Session(w http.ResponseWriter, r *http.Request) {
	resp := sessionResp{NeedSetup: a.needSetup()}
	if _, exp, ok := a.currentSession(r); ok {
		resp.Authenticated = true
		resp.ExpiresAt = exp
		resp.DeviceLabel = deviceLabel(r)
	}
	JSON(w, http.StatusOK, resp)
}

type loginReq struct {
	Password string `json:"password"`
}

// Login 校验口令；首次调用时（库里无哈希）把请求里的口令当作新口令设置。
func (a *App) Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, CodeBadRequest, "请求体不是合法 JSON")
		return
	}
	if req.Password == "" || len(req.Password) > 256 {
		Error(w, http.StatusBadRequest, CodeBadRequest, "口令不能为空且不超过 256 字符")
		return
	}

	key := clientIP(r)
	now := time.Now()
	if a.limiter != nil {
		if ok, wait := a.limiter.Allow(key, now); !ok {
			secs := int(wait.Seconds()) + 1
			w.Header().Set("Retry-After", itoaInt(secs))
			Error(w, http.StatusTooManyRequests, CodeAuthLocked, "尝试过于频繁，请稍后再试")
			return
		}
	}

	if a.needSetup() {
		hash, err := auth.Hash(req.Password)
		if err != nil {
			Error(w, http.StatusInternalServerError, CodeInternal, "无法保存口令")
			return
		}
		if a.store == nil {
			Error(w, http.StatusServiceUnavailable, CodeServiceUnavail, "状态库不可用，无法保存口令")
			return
		}
		if err := a.store.PutSecret(secretPassword, hash); err != nil {
			a.log.Error("save password failed", "err", err)
			Error(w, http.StatusInternalServerError, CodeInternal, "无法保存口令")
			return
		}
		a.hash.Store(hash)
		a.log.Info("口令已设置", "device", deviceLabel(r), "remote", key)
	} else if !auth.Verify(a.currentHash(), req.Password) {
		if a.limiter != nil {
			a.limiter.Fail(key, now)
		}
		a.log.Warn("登录失败", "device", deviceLabel(r), "remote", key)
		Error(w, http.StatusUnauthorized, CodeBadPassword, "口令不正确")
		return
	}

	if a.limiter != nil {
		a.limiter.Reset(key)
	}
	if a.sess == nil {
		Error(w, http.StatusServiceUnavailable, CodeServiceUnavail, "会话未就绪")
		return
	}
	token, exp := a.sess.Issue(deviceID(r), now)
	days := a.sess.Days
	if days <= 0 {
		days = 30
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   days * 86400,
		// Secure 不能加：默认是明文 HTTP，加了浏览器就不会回传 Cookie。
	})
	a.log.Info("登录成功", "device", deviceLabel(r), "remote", key)
	JSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"deviceLabel": deviceLabel(r),
		"expiresAt":   exp,
	})
}

// Logout 只清当前浏览器的 Cookie。
func (a *App) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// LogoutAll 轮换会话密钥：所有已发出的 Cookie 立即失效。
func (a *App) LogoutAll(w http.ResponseWriter, r *http.Request) {
	if a.store == nil || a.sess == nil {
		Error(w, http.StatusServiceUnavailable, CodeServiceUnavail, "状态库不可用")
		return
	}
	secret, err := auth.RandomSecret()
	if err != nil {
		Error(w, http.StatusInternalServerError, CodeInternal, "无法生成新密钥")
		return
	}
	if err := a.store.PutSecret(secretSessionKey, encodeSecret(secret)); err != nil {
		Error(w, http.StatusInternalServerError, CodeInternal, "无法保存新密钥")
		return
	}
	a.sess.Secret = secret
	now := time.Now().UnixMilli()
	a.log.Info("已轮换会话密钥，所有设备需重新登录")
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1})
	JSON(w, http.StatusOK, map[string]any{"ok": true, "revokedAt": now})
}

// 小工具：避免为了一个整数 import strconv
func itoaInt(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

var errNoSecret = errors.New("缺少会话密钥")
