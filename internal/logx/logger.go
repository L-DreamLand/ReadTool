// Package logx 组装 slog：级别解析、text/json 双格式、双写（文件 + stderr）。
//
// 依赖约束（设计规格 R4）：只依赖标准库与 internal/platform。
package logx

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"readtool/internal/platform"
)

// Options 是 New 的输入，字段来自 [log] 配置段。
type Options struct {
	Dir      string // 日志目录；空表示只写 stderr
	Level    string // debug | info | warn | error
	Format   string // text | json
	KeepDays int    // 启动时清理超过该天数的日志
	// Stderr 便于测试注入；为 nil 时用 os.Stderr。
	Stderr io.Writer
}

// New 返回 logger 与需要关闭的资源。即使返回错误，logger 也保证可用（降级为 stderr）。
func New(o Options) (*slog.Logger, io.Closer, error) {
	level, err := ParseLevel(o.Level)
	if err != nil {
		return nil, nil, err
	}

	stderr := o.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	var writers []io.Writer
	writers = append(writers, stderr)

	var closer io.Closer = nopCloser{}
	var fileErr error
	if strings.TrimSpace(o.Dir) != "" {
		df, openErr := platform.OpenDaily(o.Dir, o.KeepDays)
		if openErr != nil {
			// 日志目录不可写不应阻止服务启动：ReadTool 的主职责是阅读，不是记日志。
			fileErr = openErr
		} else {
			writers = append(writers, df)
			closer = df
		}
	}

	out := io.MultiWriter(writers...)
	handlerOpts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(o.Format)) {
	case "json":
		handler = slog.NewJSONHandler(out, handlerOpts)
	default:
		handler = slog.NewTextHandler(out, handlerOpts)
	}

	return slog.New(handler), closer, fileErr
}

// ParseLevel 把配置字符串转成 slog.Level。
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("log.level = %q 无效：可选 debug / info / warn / error", s)
	}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
