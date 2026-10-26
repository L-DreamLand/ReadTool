package server

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// busyAddr 占住一个随机端口，返回其地址与关闭函数。
func busyAddr(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("无法占用端口：%v", err)
	}
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func testServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
}

// testLogger 丢弃日志：测试只想看行为，不想看噪音。
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestPartialFailureKeepsRunning 覆盖 AC-20-2：一个地址失败不影响其他地址。
func TestPartialFailureKeepsRunning(t *testing.T) {
	busy, closeBusy := busyAddr(t)
	defer closeBusy()

	// 一个必然失败的地址（已被占用）+ 一个可用的地址（端口 0 由系统分配）
	mgr := NewManager([]string{busy, "127.0.0.1:0"}, 0, testServer(), testLogger())
	if err := mgr.Start(); err != nil {
		t.Fatalf("部分失败不应导致启动失败：%v", err)
	}
	defer func() { _ = mgr.Close() }()

	statuses := mgr.Status()
	if len(statuses) != 2 {
		t.Fatalf("应返回 2 条监听状态，实际 %d", len(statuses))
	}
	if statuses[0].OK {
		t.Fatalf("被占用地址不应报告成功：%+v", statuses[0])
	}
	if statuses[0].Err == "" {
		t.Fatal("失败项必须带 err，不能静默（AC-20-4）")
	}
	if !statuses[1].OK {
		t.Fatalf("可用地址应绑定成功：%+v", statuses[1])
	}
	if statuses[1].Addr == "127.0.0.1:0" {
		t.Fatalf("成功项应报告实际端口而非配置值：%s", statuses[1].Addr)
	}
	if !mgr.AnyFailed() || !mgr.AnyOK() {
		t.Fatal("混合状态判定错误")
	}
}

// TestAllFailReturnsError 覆盖：全部失败才算启动失败。
func TestAllFailReturnsError(t *testing.T) {
	a, closeA := busyAddr(t)
	defer closeA()
	b, closeB := busyAddr(t)
	defer closeB()

	mgr := NewManager([]string{a, b}, 0, testServer(), testLogger())
	err := mgr.Start()
	if err == nil {
		_ = mgr.Close()
		t.Fatal("全部绑定失败时 Start 必须返回错误")
	}
	if !strings.Contains(err.Error(), "全部监听地址均绑定失败") {
		t.Fatalf("错误信息应说明全部失败，实际：%v", err)
	}
}

// TestSummaryOnlyPrintsBoundAddresses 覆盖 AC-20-3：摘要不得出现未绑定的地址。
func TestSummaryOnlyPrintsBoundAddresses(t *testing.T) {
	busy, closeBusy := busyAddr(t)
	defer closeBusy()

	mgr := NewManager([]string{busy, "127.0.0.1:0"}, 0, testServer(), testLogger())
	if err := mgr.Start(); err != nil {
		t.Fatalf("启动失败：%v", err)
	}
	defer func() { _ = mgr.Close() }()

	var buf bytes.Buffer
	mgr.PrintSummary(&buf)
	out := buf.String()

	if !strings.Contains(out, "=== ReadTool 已启动") {
		t.Fatalf("摘要缺少标题：%s", out)
	}
	if !strings.Contains(out, "✓") {
		t.Fatalf("摘要应包含成功地址：%s", out)
	}
	if !strings.Contains(out, "✗ "+busy) {
		t.Fatalf("摘要应列出失败地址 %s：%s", busy, out)
	}
}
