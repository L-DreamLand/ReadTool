package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

// CookieName 是会话 Cookie 名。
const CookieName = "rt_session"

// ErrInvalidSession 表示 Cookie 缺失、被改、或已过期。
var ErrInvalidSession = errors.New("会话无效")

// Session 签发与校验会话 Cookie。
//
// 形态：base64(payload).base64(HMAC(secret, payload))
// payload = deviceID|issuedAtMs|expiresAtMs
// 服务端不存会话表：换密钥（logout-all）即让所有已发出的 Cookie 失效。
type Session struct {
	Secret []byte
	Days   int
}

// Issue 签发一个会话。
func (s *Session) Issue(deviceID string, now time.Time) (token string, expiresAt int64) {
	days := s.Days
	if days <= 0 {
		days = 30
	}
	exp := now.Add(time.Duration(days) * 24 * time.Hour).UnixMilli()
	payload := deviceID + "|" + strconv.FormatInt(now.UnixMilli(), 10) + "|" + strconv.FormatInt(exp, 10)
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return body + "." + s.sign(body), exp
}

// Parse 校验并解出设备标识。
func (s *Session) Parse(token string, now time.Time) (deviceID string, expiresAt int64, err error) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok || body == "" || sig == "" {
		return "", 0, ErrInvalidSession
	}
	if !hmac.Equal([]byte(sig), []byte(s.sign(body))) {
		return "", 0, ErrInvalidSession
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return "", 0, ErrInvalidSession
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return "", 0, ErrInvalidSession
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", 0, ErrInvalidSession
	}
	if now.UnixMilli() >= exp {
		return "", 0, ErrInvalidSession
	}
	return parts[0], exp, nil
}

func (s *Session) sign(payload string) string {
	mac := hmac.New(sha256.New, s.Secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
