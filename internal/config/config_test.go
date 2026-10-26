package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeFillsListenFromPort(t *testing.T) {
	c := Default()
	c.Normalize()
	if len(c.Server.Listen) != 1 || c.Server.Listen[0] != "0.0.0.0:8123" {
		t.Fatalf("默认 listen 应为 [0.0.0.0:8123]，实际 %v", c.Server.Listen)
	}

	c2 := Default()
	c2.Server.Port = 9000
	c2.Normalize()
	if c2.Server.Listen[0] != "0.0.0.0:9000" {
		t.Fatalf("只改 port 时应生成 0.0.0.0:9000，实际 %v", c2.Server.Listen)
	}
}

func TestValidateListenRejectsWildcardPlusConcrete(t *testing.T) {
	err := validateListen([]string{"0.0.0.0:8123", "192.168.1.10:8123"})
	if err == nil {
		t.Fatal("通配 + 具体地址必须被拒绝")
	}
	msg := err.Error()
	// AC-20-6：错误信息必须给出可直接复制的修法
	if !strings.Contains(msg, "二选一") || !strings.Contains(msg, `"192.168.1.10:8123"`) {
		t.Fatalf("错误信息缺少可复制的修法：%s", msg)
	}
}

func TestValidateListenAccepts(t *testing.T) {
	cases := [][]string{
		{"0.0.0.0:8123"},
		{":8123"},
		{"8123"},
		{"192.168.1.10:8123", "10.144.144.5:8123"},
		{"localhost:8123"},
	}
	for _, in := range cases {
		if err := validateListen(in); err != nil {
			t.Errorf("%v 应合法，却报错：%v", in, err)
		}
	}
}

func TestValidateListenRejectsBadFormat(t *testing.T) {
	for _, in := range [][]string{{"192.168.1.10"}, {"not a host:80x"}, {""}} {
		if err := validateListen(in); err == nil {
			t.Errorf("%v 应被拒绝", in)
		}
	}
}

func TestValidateRejectsCardViewNotBelowMinCell(t *testing.T) {
	c := Default()
	c.KB.Root = t.TempDir()
	c.Normalize()
	c.UI.MinCellPx = 130
	c.UI.CardViewPx = 130
	if _, err := Validate(c); err == nil {
		t.Fatal("card_view_px >= min_cell_px 必须报错")
	}
}

func TestValidateRejectsShortInitPassword(t *testing.T) {
	c := Default()
	c.KB.Root = t.TempDir()
	c.Server.InitPassword = "1234567"
	c.Normalize()
	if _, err := Validate(c); err == nil {
		t.Fatal("7 位初始口令必须报错")
	}
}

func TestValidateRejectsMissingKBRoot(t *testing.T) {
	c := Default()
	c.Normalize()
	if _, err := Validate(c); err == nil {
		t.Fatal("kb.root 为空必须报错")
	}
}

func TestLoadGeneratesConfigOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	kb := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	res, err := Load(Options{
		DefaultPath:       path,
		GenerateIfMissing: true,
		KBRoot:            kb,
	})
	if err != nil {
		t.Fatalf("首次启动应生成配置并成功加载：%v", err)
	}
	if !res.Generated {
		t.Fatal("应标记 Generated=true")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("配置文件未落盘：%v", err)
	}

	// 第二次加载应识别文件、且不覆盖用户改动
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	custom := strings.Replace(string(before), "rebind_interval_s = 30", "rebind_interval_s = 45", 1)
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	res2, err := Load(Options{DefaultPath: path})
	if err != nil {
		t.Fatalf("第二次加载失败：%v", err)
	}
	if res2.Generated {
		t.Fatal("第二次不应重新生成")
	}
	if res2.Cfg.Server.RebindIntervalS != 45 {
		t.Fatalf("用户改动的值未生效，实际 %d", res2.Cfg.Server.RebindIntervalS)
	}
}

func TestLoadPortFlagOverridesListen(t *testing.T) {
	dir := t.TempDir()
	kb := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[server]
listen = ["192.168.1.10:8123"]
[kb]
root = "` + strings.ReplaceAll(kb, `\`, `/`) + `"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Load(Options{ConfigPath: path, Port: 9000})
	if err != nil {
		t.Fatalf("加载失败：%v", err)
	}
	if len(res.Cfg.Server.Listen) != 1 || res.Cfg.Server.Listen[0] != "0.0.0.0:9000" {
		t.Fatalf("-port 应改写默认监听地址，实际 %v", res.Cfg.Server.Listen)
	}
}
