package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Validate 实现设计规格 §3.7 的 V1–V8。
// 约定：错误信息必须包含"怎么改"，因为它是用户唯一能看到的诊断（AC-20-6）。
func Validate(c *Config) (warnings []string, err error) {
	// V1 kb.root 必须存在且是目录
	if strings.TrimSpace(c.KB.Root) == "" {
		return warnings, fmt.Errorf("kb.root 未设置：请填写知识库根目录的绝对路径，例如\n" +
			"  [kb]\n  root = \"D:/Knowledge-Base\"\n" +
			"或用命令行临时指定：readtool -kb D:\\Knowledge-Base")
	}
	rootAbs, absErr := filepath.Abs(c.KB.Root)
	if absErr != nil {
		return warnings, fmt.Errorf("kb.root 路径无法解析（%s）：%w", c.KB.Root, absErr)
	}
	info, statErr := os.Stat(rootAbs)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return warnings, fmt.Errorf("kb.root 指向的目录不存在：%s\n"+
				"请检查路径，或先确认知识库是否被移动（可用 `readtool doctor` 查看）", rootAbs)
		}
		return warnings, fmt.Errorf("kb.root 无法访问：%s（%v）", rootAbs, statErr)
	}
	if !info.IsDir() {
		return warnings, fmt.Errorf("kb.root 必须是目录，但它是文件：%s", rootAbs)
	}

	// V2 listen 每项可被 SplitHostPort 解析
	// V3 不得同时含通配地址与具体地址
	if vErr := validateListen(c.Server.Listen); vErr != nil {
		return warnings, vErr
	}

	// V4 port 与 listen 冲突时不报错：listen 优先并 warning
	if len(c.Server.Listen) > 0 && c.Server.Port != 0 && c.Server.Port != 8123 {
		if !listenPortsInclude(c.Server.Listen, c.Server.Port) {
			warnings = append(warnings, fmt.Sprintf(
				"server.port = %d 被忽略：listen 非空时以 listen 为准（当前 %v）",
				c.Server.Port, c.Server.Listen))
		}
	}
	if c.Server.Port < 0 || c.Server.Port > 65535 {
		return warnings, fmt.Errorf("server.port = %d 越界：合法范围 1–65535", c.Server.Port)
	}

	// V5 card_view_px < min_cell_px
	if c.UI.CardViewPx >= c.UI.MinCellPx {
		return warnings, fmt.Errorf(
			"ui.card_view_px (%d) 必须小于 ui.min_cell_px (%d)：\n"+
				"  两者之间是「表格 + 横滑」的过渡档，相等会让该档消失。\n"+
				"  建议保留默认 90 / 130",
			c.UI.CardViewPx, c.UI.MinCellPx)
	}

	// V6 TLS 开启时证书与私钥可读
	if c.Security.TLSEnabled {
		for _, f := range []struct {
			name, path string
		}{
			{"security.tls_cert", c.Security.TLSCert},
			{"security.tls_key", c.Security.TLSKey},
		} {
			if strings.TrimSpace(f.path) == "" {
				return warnings, fmt.Errorf("%s 为空但 tls_enabled = true：两者必须同时给出可读的文件路径", f.name)
			}
			if _, err := os.Stat(f.path); err != nil {
				return warnings, fmt.Errorf("%s 不可读：%s（%v）", f.name, f.path, err)
			}
		}
	}

	// V7 default_font_px 在 15–24
	if c.UI.DefaultFontPx < 15 || c.UI.DefaultFontPx > 24 {
		return warnings, fmt.Errorf("ui.default_font_px = %d 越界：合法区间 15–24（平板基线建议 19）",
			c.UI.DefaultFontPx)
	}

	// V8 init_password 非空时长度 >= 8
	if pw := c.Server.InitPassword; pw != "" {
		if n := len([]rune(pw)); n < 8 {
			return warnings, fmt.Errorf("server.init_password 长度 %d 太短：至少 8 位（建议 12 位以上），"+
				"或留空让程序随机生成", n)
		}
	}

	for _, spec := range c.Server.Listen {
		_, portStr, _ := net.SplitHostPort(normalizeListen(spec))
		if p, convErr := strconv.Atoi(portStr); convErr == nil && (p < 1 || p > 65535) {
			return warnings, fmt.Errorf("listen %q 的端口 %d 越界：合法范围 1–65535", spec, p)
		}
	}

	return warnings, nil
}

// validateListen 是设计规格 §3.7 V2/V3 与专项 A.1 的实现。
// 关键：通配地址（0.0.0.0 / :: / 空 host）与具体地址不能同时出现——
// 语义重叠会让人无法判断"到底想绑哪些网卡"，且是 EasyTier 场景最常见的配置错误。
func validateListen(specs []string) error {
	if len(specs) == 0 {
		return fmt.Errorf("server.listen 为空：请至少给一个地址，例如\n" +
			"  listen = [\"0.0.0.0:8123\"]                    # 绑全部网卡\n" +
			"  listen = [\"192.168.1.10:8123\", \"10.144.144.5:8123\"]   # 只绑指定网卡（局域网 + EasyTier）")
	}

	var wildcard, concrete []string
	for _, raw := range specs {
		s := strings.TrimSpace(raw)
		host, port, err := net.SplitHostPort(normalizeListen(s))
		if err != nil {
			return fmt.Errorf("listen %q 格式错误：应为 IP:端口 或 主机名:端口（%v）\n"+
				"  合法示例：\"0.0.0.0:8123\"、\"192.168.1.10:8123\"、\":8123\"、\"localhost:8123\"", raw, err)
		}
		if port == "" {
			return fmt.Errorf("listen %q 缺少端口：应为 IP:端口", raw)
		}
		if _, portErr := strconv.Atoi(port); portErr != nil {
			return fmt.Errorf("listen %q 的端口 %q 不是数字：应为 IP:端口，例如 \"192.168.1.10:8123\"", raw, port)
		}
		if strings.ContainsAny(host, " \t/") {
			return fmt.Errorf("listen %q 的主机部分 %q 含非法字符（空格或斜杠）：应为 IP 或主机名", raw, host)
		}
		if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
			wildcard = append(wildcard, raw)
		} else {
			concrete = append(concrete, raw)
		}
	}

	if len(wildcard) > 0 && len(concrete) > 0 {
		return fmt.Errorf(
			"listen 同时包含通配地址 %v（等价于绑全部网卡）与具体地址 %v：语义重叠，无法判断意图。\n"+
				"请二选一：\n"+
				"  · 只想绑指定网卡（局域网 + EasyTier）→ listen = [%s]\n"+
				"  · 想绑全部网卡（含以后新增的虚拟网卡）→ listen = [\"0.0.0.0:%s\"]",
			wildcard, concrete,
			quoteJoin(concrete), portOf(wildcard[0]))
	}
	return nil
}

// normalizeListen 把 ":8123" / "8123" 补成 "0.0.0.0:8123"，便于 SplitHostPort。
func normalizeListen(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if strings.HasPrefix(s, ":") {
		return "0.0.0.0" + s
	}
	if _, _, err := net.SplitHostPort(s); err == nil {
		return s
	}
	if _, err := strconv.Atoi(s); err == nil {
		return "0.0.0.0:" + s
	}
	return s
}

func quoteJoin(items []string) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, strconv.Quote(it))
	}
	return strings.Join(parts, ", ")
}

func portOf(spec string) string {
	_, port, err := net.SplitHostPort(normalizeListen(spec))
	if err != nil {
		return "8123"
	}
	return port
}

func listenPortsInclude(specs []string, port int) bool {
	for _, s := range specs {
		if p, err := strconv.Atoi(portOf(s)); err == nil && p == port {
			return true
		}
	}
	return false
}
