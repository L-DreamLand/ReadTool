// Package platform 负责"本机事实"：数据目录、配置文件位置、按天日志文件。
//
// 依赖约束（设计规格 R4）：只依赖标准库，不 import 任何 other internal 包。
package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// appDirName 是 Windows 下 %LOCALAPPDATA% 里的目录名；类 Unix 下用小写短名。
func appDirs() (dataName string) {
	if runtime.GOOS == "windows" {
		return "ReadTool"
	}
	return "readtool"
}

// DataDir 返回状态目录：
//   - Windows: %LOCALAPPDATA%\ReadTool
//   - 其他:    $XDG_DATA_HOME/readtool 或 ~/.local/share/readtool
//
// 只计算路径，不创建目录（调用方按需 MkdirAll，便于区分"不可写"与"不存在"）。
func DataDir() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			if home, err := os.UserHomeDir(); err == nil {
				base = filepath.Join(home, "AppData", "Local")
			}
		}
		if base == "" {
			return "", fmt.Errorf("无法确定数据目录：环境变量 LOCALAPPDATA 为空且找不到用户主目录")
		}
		return filepath.Join(base, appDirs()), nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, appDirs()), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法确定数据目录：%w", err)
	}
	return filepath.Join(home, ".local", "share", appDirs()), nil
}

// ConfigPath 返回默认配置文件路径（数据目录下的 config.toml）。
func ConfigPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// StatePath 返回状态库路径（knowledge base 之外，物理分离）。
func StatePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.db"), nil
}

// LogsDir 返回日志目录（数据目录下的 logs/）。
func LogsDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "logs"), nil
}

// EnsureDir 创建目录（幂等），并返回解析后的绝对路径。
func EnsureDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("无法解析目录路径 %s：%w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("无法创建目录 %s：%w", abs, err)
	}
	return abs, nil
}
