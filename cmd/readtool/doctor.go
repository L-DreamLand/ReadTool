package main

import (
	"flag"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"readtool/internal/config"
	"readtool/internal/platform"
)

// cmdDoctor 实现 `readtool doctor` 的 M0 版本：把"连不上"的排查顺序固化下来。
// 设计规格 §8 要求最终必须区分「绑定失败」与「防火墙拦截」，并打印 NetworkCategory；
// 那部分（Windows 网络位置）属于 M3，这里先给出网卡、端口、知识库、配置四项事实。
func cmdDoctor(args []string) int {
	fsx := flag.NewFlagSet("readtool doctor", flag.ContinueOnError)
	fsx.SetOutput(os.Stderr)
	cfgPath := fsx.String("config", "", "配置文件路径")
	kb := fsx.String("kb", "", "知识库根目录")
	if err := fsx.Parse(args); err != nil {
		return 2
	}

	problems := 0
	section := func(title string) { fmt.Printf("\n=== %s ===\n", title) }

	section("版本")
	fmt.Printf("  readtool   %s\n", version)

	section("平台目录")
	dataDir, dataErr := platform.DataDir()
	cfgDefault, _ := platform.ConfigPath()
	logsDir, _ := platform.LogsDir()
	fmt.Printf("  数据目录   %s\n", orUnknown(dataDir, dataErr))
	fmt.Printf("  配置默认   %s\n", cfgDefault)
	fmt.Printf("  日志目录   %s\n", logsDir)
	if dataErr == nil {
		if _, err := platform.EnsureDir(dataDir); err != nil {
			fmt.Printf("  可写性     ✗ %v\n", err)
			problems++
		} else {
			fmt.Printf("  可写性     ✓\n")
		}
	}

	section("配置")
	path := *cfgPath
	if path == "" {
		path = cfgDefault
	}
	if _, err := os.Stat(path); err != nil {
		fmt.Printf("  文件       %s（不存在，首次启动会自动生成）\n", path)
	} else {
		fmt.Printf("  文件       %s\n", path)
	}
	// doctor 不应该因为"还没配置"就报错退出：它的职责就是告诉你怎么配置。
	loadOpts := config.Options{KBRoot: *kb}
	if _, statErr := os.Stat(path); statErr == nil {
		loadOpts.ConfigPath = path
	}
	res, err := config.Load(loadOpts)
	if err != nil {
		fmt.Printf("  加载       ✗ %v\n", err)
		problems++
		res.Cfg = config.Default()
		if *kb != "" {
			res.Cfg.KB.Root = *kb
		}
	}
	cfg := res.Cfg
	if cfg != nil {
		fmt.Printf("  kb.root    %s\n", orUnknownStr(cfg.KB.Root))
		fmt.Printf("  listen     %v\n", cfg.Server.Listen)
		fmt.Printf("  重试周期   %ds\n", cfg.Server.RebindIntervalS)
		for _, w := range res.Warnings {
			fmt.Printf("  ⚠ %s\n", w)
		}
	}

	section("知识库")
	if cfg != nil && strings.TrimSpace(cfg.KB.Root) != "" {
		root, err := filepath.Abs(cfg.KB.Root)
		if err != nil {
			fmt.Printf("  ✗ 路径无法解析：%v\n", err)
			problems++
		} else {
			mdCount, totalBytes, scanErr := scanMarkdown(root)
			if scanErr != nil {
				fmt.Printf("  ✗ %v\n", scanErr)
				problems++
			} else {
				fmt.Printf("  根目录     %s\n", root)
				fmt.Printf("  markdown   %d 篇（%.1f KB）\n", mdCount, float64(totalBytes)/1024)
			}
		}
	} else {
		fmt.Printf("  ! 尚未配置 kb.root\n")
	}

	section("网卡地址")
	printInterfaces()

	if cfg != nil {
		section("监听端口可用性")
		for _, spec := range cfg.Server.Listen {
			ln, err := net.Listen("tcp", spec)
			if err != nil {
				fmt.Printf("  ✗ %-28s %v\n", spec, err)
				problems++
				continue
			}
			fmt.Printf("  ✓ %-28s 可绑定（%s）\n", spec, ln.Addr())
			_ = ln.Close()
		}
		fmt.Printf("\n建议配置（局域网 + EasyTier 虚拟网卡各一条）：\n")
		fmt.Printf("  [server]\n  listen = [%s]\n", suggestedListen(portOf(cfg)))
	}

	fmt.Printf("\n=== 结论 ===\n")
	if problems == 0 {
		fmt.Printf("  未发现阻塞问题。\n")
	} else {
		fmt.Printf("  发现 %d 个问题，见上方 ✗ 行。\n", problems)
	}
	if runtime.GOOS == "windows" {
		fmt.Printf("  提示：若地址能绑定但平板连不上，检查 Windows 防火墙入站规则与网卡「网络位置」\n")
		fmt.Printf("        （公用网络默认阻止入站，需 -Profile Any 放行 8123）。\n\n")
	} else {
		fmt.Printf("  提示：若地址能绑定但平板连不上，检查本机防火墙（nftables/ufw/firewalld）是否放行该端口，\n")
		fmt.Printf("        以及路由器是否开了 AP 隔离（这时走 EasyTier 虚拟网络更省事）。\n\n")
	}

	if problems > 0 {
		return 1
	}
	return 0
}

// scanMarkdown 只统计，不读取内容——doctor 不该因为某篇超大文件而变慢。
func scanMarkdown(root string) (count int, total int64, err error) {
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // 单个目录不可读不影响整体结论
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		count++
		total += info.Size()
		return nil
	})
	return count, total, err
}

func printInterfaces() {
	ifaces, err := net.Interfaces()
	if err != nil {
		fmt.Printf("  读取失败：%v\n", err)
		return
	}
	type row struct{ name, addrs, flags string }
	var rows []row
	for _, iface := range ifaces {
		addrs, addrErr := iface.Addrs()
		if addrErr != nil || len(addrs) == 0 {
			continue
		}
		var v4, v6 []string
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP
			if ip.To4() != nil {
				v4 = append(v4, ipnet.String())
				continue
			}
			if !ip.IsLinkLocalUnicast() {
				v6 = append(v6, ipnet.String())
			}
		}
		var flags []string
		if iface.Flags&net.FlagUp == 0 {
			flags = append(flags, "down")
		}
		if iface.Flags&net.FlagLoopback != 0 {
			flags = append(flags, "loopback")
		}
		all := append(append([]string{}, v4...), v6...)
		if len(all) == 0 {
			continue
		}
		rows = append(rows, row{
			name:  iface.Name,
			addrs: strings.Join(all, " "),
			flags: strings.Join(flags, ","),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	for _, r := range rows {
		mark := ""
		if r.flags != "" {
			mark = "  [" + r.flags + "]"
		}
		fmt.Printf("  %-20s %s%s\n", r.name, r.addrs, mark)
	}
}

// suggestedListen 挑出"最像局域网"与"最像虚拟网卡"的两个地址各给一条建议。
// 只是建议，永远不自动写入配置。
// portOf 取配置里的端口，用于拼建议地址（换过 -port 或改过 config 时不能还写 8123）。
// 优先从 listen 里取：用户往往只改 listen，port 这个"兜底项"会一直是 8123，
// 用它拼建议会给出一个自己都没在听的端口。
func portOf(cfg *config.Config) int {
	if cfg != nil {
		for _, spec := range cfg.Server.Listen {
			if _, port, err := net.SplitHostPort(spec); err == nil {
				if n, convErr := strconv.Atoi(port); convErr == nil && n > 0 {
					return n
				}
			}
		}
		if cfg.Server.Port > 0 {
			return cfg.Server.Port
		}
	}
	return 8123
}

func suggestedListen(port int) string {
	tail := fmt.Sprintf(":%d", port)
	ifaces, err := net.Interfaces()
	if err != nil {
		return fmt.Sprintf("%q", "0.0.0.0"+tail)
	}
	var candidates []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			ip := ipnet.IP.String()
			if strings.HasPrefix(ip, "169.254.") { // APIPA：没接网线
				continue
			}
			candidates = append(candidates, fmt.Sprintf("%q", ip+tail))
		}
	}
	if len(candidates) == 0 {
		return fmt.Sprintf("%q", "0.0.0.0"+tail)
	}
	if len(candidates) > 2 {
		candidates = candidates[:2]
	}
	return strings.Join(candidates, ", ")
}

func orUnknown(s string, err error) string {
	if err != nil {
		return "(失败) " + err.Error()
	}
	return s
}

func orUnknownStr(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(未设置)"
	}
	return s
}
