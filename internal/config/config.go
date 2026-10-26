// Package config 负责 TOML 配置的加载、默认值、校验与默认配置生成。
//
// 依赖约束（设计规格 R4）：本包不 import 任何其他 internal 包，
// 因此"配置文件该放哪"由调用方（cmd）通过 Options.DefaultPath 传入。
package config

// Config 与设计规格 §3 的配置表一一对应。
type Config struct {
	Server   ServerConfig   `toml:"server"`
	KB       KBConfig       `toml:"kb"`
	UI       UIConfig       `toml:"ui"`
	Security SecurityConfig `toml:"security"`
	Log      LogConfig      `toml:"log"`
}

// ServerConfig 对应 [server]。
type ServerConfig struct {
	// Listen 是多地址监听列表，每项 `IP:端口` / `主机名:端口` / `:端口`。
	// 空数组表示使用 Port 构造的默认值（0.0.0.0:Port）。
	Listen []string `toml:"listen"`
	// RebindIntervalS 是绑定失败后的重试周期，0 表示关闭自愈。
	RebindIntervalS int `toml:"rebind_interval_s"`
	// InitPassword 只在 state.db 尚无口令哈希时生效，用于首次设置口令。
	InitPassword string `toml:"init_password"`
	// Port 仅在 Listen 为空时参与构造默认监听地址。
	Port int `toml:"port"`
}

// KBConfig 对应 [kb]。
type KBConfig struct {
	Root          string `toml:"root"`
	ShowWorkspace bool   `toml:"show_workspace"`
	IncludeRootMD bool   `toml:"include_root_md"`
	Watch         bool   `toml:"watch"`
	DebounceMs    int    `toml:"debounce_ms"`
	MaxFileMB     int    `toml:"max_file_mb"`
}

// UIConfig 对应 [ui]，全部为"下次请求即生效"的热生效项。
type UIConfig struct {
	DefaultTheme  string `toml:"default_theme"`
	DefaultFontPx int    `toml:"default_font_px"`
	MinCellPx     int    `toml:"min_cell_px"`
	CardViewPx    int    `toml:"card_view_px"`
	DrawerPx      int    `toml:"drawer_px"`
	ContentMaxCh  int    `toml:"content_max_ch"`
}

// SecurityConfig 对应 [security]。
type SecurityConfig struct {
	SessionDays      int    `toml:"session_days"`
	LoginMaxAttempts int    `toml:"login_max_attempts"`
	LoginWindowS     int    `toml:"login_window_s"`
	LoginLockS       int    `toml:"login_lock_s"`
	TLSEnabled       bool   `toml:"tls_enabled"`
	TLSCert          string `toml:"tls_cert"`
	TLSKey           string `toml:"tls_key"`
}

// LogConfig 对应 [log]。
type LogConfig struct {
	Level    string `toml:"level"`
	Format   string `toml:"format"`
	KeepDays int    `toml:"keep_days"`
}
