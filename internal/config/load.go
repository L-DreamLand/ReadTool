package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Options 是 Load 的输入。命令行参数优先级高于配置文件（设计规格 §3.6）。
type Options struct {
	// ConfigPath 显式指定的配置文件（-config）。
	ConfigPath string
	// DefaultPath 是配置文件默认位置，由 cmd 用 platform.ConfigPath() 传入
	// （config 包不 import platform，见设计规格 R4）。
	DefaultPath string
	// GenerateIfMissing 为 true 时，目标配置文件不存在则写出一份带注释的默认配置。
	GenerateIfMissing bool

	// 以下为命令行覆盖项（零值表示"未指定"）。
	KBRoot   string
	Listen   string // 逗号分隔
	Port     int
	LogLevel string
}

// Result 是 Load 的输出，附带"实际用了哪个文件"这类可打印的事实。
type Result struct {
	Cfg       *Config
	Path      string   // 实际加载的配置文件；空表示全部走内置默认
	Generated bool     // 本次是否新生成配置文件
	Warnings  []string // 非致命提示（未识别的键、port 被忽略等）
}

// Load 依次完成：定位文件 → 必要时生成默认配置 → 解码 TOML → 应用命令行覆盖
// → 补默认值 → 校验。任何一步失败都返回 error，由 cmd 打印后以码 2 退出。
func Load(opts Options) (Result, error) {
	var res Result
	cfg := Default()

	path := strings.TrimSpace(opts.ConfigPath)
	if path == "" {
		path = strings.TrimSpace(opts.DefaultPath)
	}

	if path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return res, fmt.Errorf("配置文件路径无法解析（%s）：%w", path, err)
		}
		path = abs
		// 立刻记到 res 上：后续任何一步失败（生成后校验不过），
		// cmd 都需要知道"配置文件到底是哪个"，否则提示里路径是空的。
		res.Path = path

		switch _, statErr := os.Stat(path); {
		case statErr == nil:
			// 正常加载
		case errors.Is(statErr, os.ErrNotExist):
			if !opts.GenerateIfMissing {
				return res, fmt.Errorf("配置文件不存在：%s\n"+
					"请去掉 -config 使用默认位置，或先用 `readtool -print-default-config > %s` 生成一份",
					path, path)
			}
			if err := writeDefaultConfig(path, opts.KBRoot); err != nil {
				return res, fmt.Errorf("无法生成默认配置 %s：%w", path, err)
			}
			res.Generated = true
		default:
			return res, fmt.Errorf("无法访问配置文件 %s：%w", path, statErr)
		}

		md, err := toml.DecodeFile(path, cfg)
		if err != nil {
			return res, fmt.Errorf("配置文件解析失败 %s：%w\n"+
				"提示：字符串必须加引号，Windows 路径中的反斜杠建议改用 / 或双写 \\\\", path, err)
		}
		for _, key := range md.Undecoded() {
			res.Warnings = append(res.Warnings, fmt.Sprintf("配置项 %s 未被识别，已忽略（检查拼写）", key.String()))
		}
	}

	// 命令行覆盖（优先级最高）
	if opts.KBRoot != "" {
		cfg.KB.Root = opts.KBRoot
	}
	if opts.Listen != "" {
		items := splitCSV(opts.Listen)
		if len(items) == 0 {
			return res, fmt.Errorf("-listen 为空：期望形如 -listen 192.168.1.10:8123,10.144.144.5:8123")
		}
		cfg.Server.Listen = items
	}
	if opts.Port != 0 {
		cfg.Server.Port = opts.Port
		// 显式给了 -port 就忽略配置文件里的 listen（语义上等价于"改端口"）
		if opts.Listen == "" && len(cfg.Server.Listen) > 0 {
			cfg.Server.Listen = nil
		}
	}
	if opts.LogLevel != "" {
		cfg.Log.Level = opts.LogLevel
	}

	cfg.Normalize()

	warn, err := Validate(cfg)
	res.Warnings = append(res.Warnings, warn...)
	if err != nil {
		return res, err
	}

	res.Cfg = cfg
	res.Path = path
	return res, nil
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// writeDefaultConfig 生成默认配置文件（含注释），目录不存在时创建。
func writeDefaultConfig(path, kbRoot string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(DefaultTOML(kbRoot)), 0o644)
}
