package config

import (
	"fmt"
	"strings"
)

// Default 返回内置默认值（设计规格 §3 各表"默认"列）。
// 注意 Listen 故意留空：它需要在 Normalize 中依据 Port 构造，
// 这样用户只写 `port = 9000` 也能得到 0.0.0.0:9000。
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Listen:          nil,
			RebindIntervalS: 30,
			InitPassword:    "",
			Port:            8123,
		},
		KB: KBConfig{
			Root:          "",
			ShowWorkspace: false,
			IncludeRootMD: true,
			Watch:         true,
			DebounceMs:    300,
			MaxFileMB:     5,
		},
		UI: UIConfig{
			DefaultTheme:  "auto",
			DefaultFontPx: 19,
			MinCellPx:     130,
			CardViewPx:    90,
			DrawerPx:      280,
			ContentMaxCh:  68,
		},
		Security: SecurityConfig{
			SessionDays:      30,
			LoginMaxAttempts: 5,
			LoginWindowS:     300,
			LoginLockS:       60,
		},
		Log: LogConfig{
			Level:    "info",
			Format:   "text",
			KeepDays: 7,
		},
	}
}

// Normalize 把"空值/零值"补成可运行的形态。只做补全，不做校验。
func (c *Config) Normalize() {
	if c.Server.Port == 0 {
		c.Server.Port = 8123
	}
	if len(c.Server.Listen) == 0 {
		c.Server.Listen = []string{fmt.Sprintf("0.0.0.0:%d", c.Server.Port)}
	}
	if c.Server.RebindIntervalS < 0 {
		c.Server.RebindIntervalS = 0
	}
	if c.KB.DebounceMs == 0 {
		c.KB.DebounceMs = 300
	}
	if c.KB.MaxFileMB == 0 {
		c.KB.MaxFileMB = 5
	}
	if c.UI.DefaultTheme == "" {
		c.UI.DefaultTheme = "auto"
	}
	if c.UI.DefaultFontPx == 0 {
		c.UI.DefaultFontPx = 19
	}
	if c.UI.MinCellPx == 0 {
		c.UI.MinCellPx = 130
	}
	if c.UI.CardViewPx == 0 {
		c.UI.CardViewPx = 90
	}
	if c.UI.DrawerPx == 0 {
		c.UI.DrawerPx = 280
	}
	if c.UI.ContentMaxCh == 0 {
		c.UI.ContentMaxCh = 68
	}
	if c.Security.SessionDays == 0 {
		c.Security.SessionDays = 30
	}
	if c.Security.LoginMaxAttempts == 0 {
		c.Security.LoginMaxAttempts = 5
	}
	if c.Security.LoginWindowS == 0 {
		c.Security.LoginWindowS = 300
	}
	if c.Security.LoginLockS == 0 {
		c.Security.LoginLockS = 60
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "text"
	}
	if c.Log.KeepDays == 0 {
		c.Log.KeepDays = 7
	}
}

// DefaultTOML 生成首次启动时落盘的带注释配置。
// kbRoot 非空时直接写入（用户用 `-kb` 指定过），否则留空并提示填写。
func DefaultTOML(kbRoot string) string {
	return fmt.Sprintf(defaultTOMLTemplate, tomlString(kbRoot))
}

// tomlString 把一个普通字符串安全地放进 TOML 基本字符串里。
//
// 必须做这件事：Windows 路径 `C:\Users\...` 直接塞进双引号会触发 `\U` 转义解析，
// TOML 解析器会直接报 "expected eight hexadecimal digits after '\U'"。
// 统一转成正斜杠——Go 的 os 包在 Windows 上同样接受 `/`，用户看起来也更清爽。
func tomlString(s string) string {
	s = strings.ReplaceAll(s, `\`, `/`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

const defaultTOMLTemplate = `# ReadTool 配置文件
# 首次启动时自动生成。修改后需重启进程才生效（ui.* 与 log.level 除外，见设计规格 §3.8）。

[server]
# 监听地址列表：每项 "IP:端口" 或 "主机名:端口" 或 ":端口"。
# 多地址用于同时挂局域网与虚拟局域网（EasyTier），逐项独立绑定，某项失败不影响其他项。
# 规则：不能同时出现 0.0.0.0（或空 host）与具体地址，二选一。
listen = ["0.0.0.0:8123"]

# 绑定失败后的重试周期（秒）。0 = 关闭自愈。
# 虚拟网卡（EasyTier）常常比本进程晚就绪，保留 30 即能在其出现后自动绑上。
rebind_interval_s = 30

# 首次设置访问口令。仅当状态库中还没有口令哈希时生效；留空则随机生成 16 位并打印一次。
init_password = ""

# 仅当 listen 为空数组时用于构造默认监听地址。
port = 8123

[kb]
# 知识库根目录（必填，启动时解析为绝对路径）。ReadTool 对它严格只读。
root = "%s"

# 是否把 _workspace/（机读索引、脚本产物）纳入目录浏览。
show_workspace = false

# 是否展示根目录的 INDEX.md / AGENTS.md。
include_root_md = true

# 知识库变更热更新（fsnotify）。
watch = true

# 变更聚合窗口（毫秒）。
debounce_ms = 300

# 单文件上限（MB），超限跳过并告警。
max_file_mb = 5

[ui]
# 首屏主题默认值：auto | light | dark | paper（护眼米黄）。客户端 localStorage 优先。
default_theme = "auto"

# 平板基线字号（px）。容器宽度 < 768px 时前端自动降到 min(17, 该值)。
default_font_px = 19

# 宽表判定：单元格实测宽度 >= min_cell_px 用表格视图。
min_cell_px = 130

# 单元格宽度 < card_view_px 时改用卡片视图。必须小于 min_cell_px。
card_view_px = 90

# 目录抽屉宽度（px），实际取 min(drawer_px, 72vw)。
drawer_px = 280

# 正文最大行宽（字符数）。
content_max_ch = 68

[security]
# 登录 Cookie 有效期（天）。
session_days = 30

# 单 IP 在窗口内的失败上限，超过后锁定 login_lock_s 秒。
login_max_attempts = 5
login_window_s = 300
login_lock_s = 60

# 可选自签 HTTPS。开启后在同一 listen 地址上换协议，不额外开端口。
tls_enabled = false
tls_cert = ""
tls_key = ""

[log]
# debug | info | warn | error
level = "info"

# text | json
format = "text"

# 启动时清理超过该天数的日志文件。
keep_days = 7
`
