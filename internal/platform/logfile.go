package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const logFilePrefix = "readtool-"
const logFileSuffix = ".log"

// DailyFile 是一个按自然日切分的追加写入器。
//
// 为什么自己实现而不用第三方轮转库：需求只有一个——按天切分 + 启动时清旧。
// 日志是排障通道，它自己不能成为故障源，代码越少越好。
type DailyFile struct {
	dir  string
	keep int

	mu   sync.Mutex
	day  string
	file *os.File
}

// OpenDaily 打开（必要时创建）当天的日志文件，并清理超期文件。
func OpenDaily(dir string, keepDays int) (*DailyFile, error) {
	abs, err := EnsureDir(dir)
	if err != nil {
		return nil, err
	}
	d := &DailyFile{dir: abs, keep: keepDays}
	if err := d.rotateLocked(time.Now()); err != nil {
		return nil, err
	}
	// 清理失败不影响启动：日志是辅助设施
	_ = d.cleanup(time.Now())
	return d, nil
}

// Write 实现 io.Writer。跨天时自动切换文件。
func (d *DailyFile) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	if day := now.Format("2006-01-02"); day != d.day {
		if err := d.rotateLocked(now); err != nil {
			return 0, err
		}
		_ = d.cleanup(now)
	}
	if d.file == nil {
		return 0, fmt.Errorf("日志文件未就绪")
	}
	return d.file.Write(p)
}

// Close 关闭当前文件。可重复调用。
func (d *DailyFile) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file == nil {
		return nil
	}
	err := d.file.Close()
	d.file = nil
	return err
}

// Path 返回当前日志文件路径（用于启动摘要打印）。
func (d *DailyFile) Path() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return filepath.Join(d.dir, logFilePrefix+d.day+logFileSuffix)
}

func (d *DailyFile) rotateLocked(now time.Time) error {
	day := now.Format("2006-01-02")
	if d.file != nil && day == d.day {
		return nil
	}
	if d.file != nil {
		_ = d.file.Close()
		d.file = nil
	}
	path := filepath.Join(d.dir, logFilePrefix+day+logFileSuffix)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("无法打开日志文件 %s：%w", path, err)
	}
	d.file = f
	d.day = day
	return nil
}

// cleanup 删除超过 keep 天的日志文件；keep <= 0 时清理功能关闭。
func (d *DailyFile) cleanup(now time.Time) error {
	if d.keep <= 0 {
		return nil
	}
	cutoff := now.AddDate(0, 0, -d.keep).Format("2006-01-02")

	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, logFilePrefix) || !strings.HasSuffix(name, logFileSuffix) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		day := strings.TrimSuffix(strings.TrimPrefix(name, logFilePrefix), logFileSuffix)
		if day < cutoff { // 文件名是 YYYY-MM-DD，字典序即时间序
			_ = os.Remove(filepath.Join(d.dir, name))
		}
	}
	return nil
}
