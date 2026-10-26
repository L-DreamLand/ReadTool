package server

import (
	"fmt"
	"io"
	"net"
	"strings"
)

// PrintSummary 打印启动摘要（专属 A.3）。
// 只打印**真正绑定成功**的地址：AC-20-3 要求"配了什么就说什么"，
// 若只配了局域网地址，摘要里绝不能出现虚拟网卡地址。
func (m *Manager) PrintSummary(w io.Writer) {
	retryNote := "已关闭自愈"
	if m.retry > 0 {
		retryNote = fmt.Sprintf("每 %s 重试", m.retry)
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "=== ReadTool 已启动，可从以下地址访问 ===")

	okCount := 0
	for _, l := range m.items {
		if l.ok.Load() {
			addr, _ := l.addr.Load().(string)
			display := normalizeDisplayAddr(addr)
			fmt.Fprintf(w, "  ✓ http://%s\n", display)
			okCount++
			continue
		}
		errText := "绑定失败"
		if err := lastErr(l); err != nil {
			errText = err.Error()
		}
		fmt.Fprintf(w, "  ✗ %-28s 绑定失败：%s（%s）\n", l.spec, errText, retryNote)
	}

	if okCount == 0 {
		fmt.Fprintln(w, "  （当前没有任何可用地址）")
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "提示：非 127.0.0.1 / 局域网网段的地址通常来自虚拟网卡（EasyTier 等），")
	fmt.Fprintln(w, "      仅在平板加入同一虚拟网络后可达；仍需要口令登录。")
	fmt.Fprintln(w, "")
}

// normalizeDisplayAddr 把 ":8123" 这类通配绑定结果改写成用户能直接点开的地址。
// net.Listen(":8123") 的 Addr() 是 "[::]:8123"，直接打印会让人以为要访问 [::]。
func normalizeDisplayAddr(addr string) string {
	if addr == "" {
		return addr
	}
	if strings.HasPrefix(addr, "[::]") || strings.HasPrefix(addr, "0.0.0.0") {
		_, port, err := net.SplitHostPort(addr)
		if err == nil {
			return "127.0.0.1:" + port
		}
		return "127.0.0.1"
	}
	return addr
}
