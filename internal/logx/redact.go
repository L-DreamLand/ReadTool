package logx

import "log/slog"

// 设计规格 §1 编码红线之三：口令与会话不得进日志。
// 这里做的是"白名单式兜底"——调用方仍应主动避免传敏感值。
var sensitiveKeys = map[string]struct{}{
	"password":      {},
	"init_password": {},
	"passwd":        {},
	"secret":        {},
	"token":         {},
	"cookie":        {},
	"session":       {},
	"rt_session":    {},
	"authorization": {},
}

const redacted = "[已隐藏]"

// Redact 返回脱敏后的值：敏感键一律替换为占位符。
// 用法：log.Info("login", logx.Redact("password", req.Password))
func Redact(key string, value any) slog.Attr {
	if _, ok := sensitiveKeys[lower(key)]; ok {
		return slog.String(key, redacted)
	}
	return slog.Any(key, value)
}

// Sensitive 报告某个键名是否属于敏感字段，供中间件判断要不要记录请求体字段。
func Sensitive(key string) bool {
	_, ok := sensitiveKeys[lower(key)]
	return ok
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
