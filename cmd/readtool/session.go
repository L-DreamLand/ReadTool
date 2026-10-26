package main

import (
	"encoding/base64"
	"log/slog"

	"readtool/internal/auth"
	"readtool/internal/config"
	"readtool/internal/platform"
	"readtool/internal/state"
)

const (
	passwordMissing     = "missing"
	passwordInitialized = "initialized"
	passwordExisting    = "existing"
)

func encodeSecret(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }

func decodeSecret(s string) ([]byte, error) { return base64.RawStdEncoding.DecodeString(s) }

// mustStatePath 返回状态库路径；失败时退回内存库（只影响书签等状态，不影响阅读）。
func mustStatePath() string {
	p, err := platform.StatePath()
	if err != nil {
		return ":memory:"
	}
	return p
}

// ensureSession 取出会话密钥，没有就生成一个写进状态库。
func ensureSession(store *state.Store, cfg *config.Config) (*auth.Session, error) {
	const key = "session_secret"
	encoded, ok, err := store.Secret(key)
	if err != nil {
		return nil, err
	}
	var secret []byte
	if ok {
		secret, err = decodeSecret(encoded)
		if err != nil || len(secret) != 32 {
			secret = nil // 被手改坏了，当作没有，重新生成
		}
	}
	if secret == nil {
		secret, err = auth.RandomSecret()
		if err != nil {
			return nil, err
		}
		if err := store.PutSecret(key, encodeSecret(secret)); err != nil {
			return nil, err
		}
	}
	return &auth.Session{Secret: secret, Days: cfg.Security.SessionDays}, nil
}

// ensurePassword 处理首次运行的口令初始化：
// 配置里给了 init_password 就直接建哈希；否则留给页面上的"设置口令"流程。
func ensurePassword(store *state.Store, cfg *config.Config, logger *slog.Logger) (string, error) {
	if _, ok, err := store.Secret("password_hash"); err != nil {
		return "", err
	} else if ok {
		return passwordExisting, nil
	}
	if cfg.Server.InitPassword == "" {
		logger.Info("尚未设置访问口令，首次打开页面时会引导设置")
		return passwordMissing, nil
	}
	hash, err := auth.Hash(cfg.Server.InitPassword)
	if err != nil {
		return "", err
	}
	if err := store.PutSecret("password_hash", hash); err != nil {
		return "", err
	}
	logger.Info("已按配置初始化访问口令")
	return passwordInitialized, nil
}
