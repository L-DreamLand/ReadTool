// Command readtool 是一个只读知识库阅读服务：本机常驻，平板经局域网/虚拟局域网浏览。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"readtool/internal/auth"
	"readtool/internal/config"
	"readtool/internal/httpapi"
	"readtool/internal/kb"
	"readtool/internal/logx"
	"readtool/internal/platform"
	"readtool/internal/render"
	"readtool/internal/server"
	"readtool/internal/state"
)

// version 由 -ldflags "-X main.version=..." 注入；开发期为 dev。
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "doctor":
			os.Exit(cmdDoctor(os.Args[2:]))
		case "version", "-v", "--version":
			fmt.Printf("readtool %s\n", version)
			return
		case "help", "-h", "--help":
			usage()
			return
		}
	}
	os.Exit(run(os.Args[1:]))
}

func usage() {
	fmt.Fprintf(os.Stderr, `ReadTool —— 局域网只读知识库阅读服务

用法：
  readtool [选项]            启动服务
  readtool doctor [选项]     诊断环境（网卡、端口、知识库、配置）
  readtool version           打印版本

选项：
  -config <路径>     配置文件（默认 %s）
  -kb <目录>         知识库根目录（覆盖配置，只读）
  -listen <列表>     监听地址，逗号分隔，如 192.168.1.10:8123,10.144.144.5:8123
  -port <端口>       监听端口（listen 为空时使用；显式给出则忽略配置里的 listen）
  -log-level <级别>  debug|info|warn|error
  -log-format <格式> text|json
  -dev               开发模式：静态资源禁用缓存
  -print-default-config  打印一份带注释的默认配置后退出

配置优先级：命令行 > 配置文件 > 内置默认。
`, defaultConfigPathHint())
}

func defaultConfigPathHint() string {
	p, err := platform.ConfigPath()
	if err != nil {
		return "<无法确定>"
	}
	return p
}

func run(args []string) int {
	fs := flag.NewFlagSet("readtool", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		flagConfig   = fs.String("config", "", "配置文件路径")
		flagKB       = fs.String("kb", "", "知识库根目录")
		flagListen   = fs.String("listen", "", "监听地址列表（逗号分隔）")
		flagPort     = fs.Int("port", 0, "监听端口")
		flagLogLevel = fs.String("log-level", "", "日志级别")
		flagLogFmt   = fs.String("log-format", "", "日志格式 text|json")
		flagDev      = fs.Bool("dev", false, "开发模式（禁用静态资源缓存）")
		flagPrintDef = fs.Bool("print-default-config", false, "打印默认配置后退出")
	)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if *flagPrintDef {
		fmt.Print(config.DefaultTOML(*flagKB))
		return 0
	}

	// S1：配置先于一切。失败必须给出"怎么改"，并以码 2 退出。
	defaultCfgPath, pathErr := platform.ConfigPath()
	if pathErr != nil {
		fmt.Fprintf(os.Stderr, "✗ 无法确定配置文件位置：%v\n", pathErr)
		return 2
	}

	res, loadErr := config.Load(config.Options{
		ConfigPath:        *flagConfig,
		DefaultPath:       defaultCfgPath,
		GenerateIfMissing: *flagConfig == "",
		KBRoot:            *flagKB,
		Listen:            *flagListen,
		Port:              *flagPort,
		LogLevel:          *flagLogLevel,
	})
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "✗ 启动失败：\n%v\n", loadErr)
		if res.Generated {
			fmt.Fprintf(os.Stderr, "\n已生成配置模板：%s\n请填写其中的 [kb] root，或直接用 -kb <目录> 指定后重新启动。\n", res.Path)
		}
		return 2
	}
	cfg := res.Cfg

	if *flagLogFmt != "" {
		cfg.Log.Format = *flagLogFmt
	}

	// S2：日志与平台目录
	logDir, dirErr := platform.LogsDir()
	if dirErr != nil {
		fmt.Fprintf(os.Stderr, "✗ 无法确定日志目录：%v\n", dirErr)
		return 2
	}
	logger, closer, logErr := logx.New(logx.Options{
		Dir:      logDir,
		Level:    cfg.Log.Level,
		Format:   cfg.Log.Format,
		KeepDays: cfg.Log.KeepDays,
	})
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}
	if logErr != nil {
		fmt.Fprintf(os.Stderr, "! 日志文件不可用，仅输出到终端：%v\n", logErr)
	}

	for _, w := range res.Warnings {
		logger.Warn("config", "msg", w)
	}

	logger.Info("readtool starting",
		"version", version,
		"config", orNone(res.Path),
		"generated", res.Generated,
		"kb_root", cfg.KB.Root,
		"listen", cfg.Server.Listen,
		"rebind_interval_s", cfg.Server.RebindIntervalS,
		"log_dir", logDir,
	)

	// S3：扫描知识库（只读，不写任何东西到 KB 目录）
	scannedAt := time.Now()
	catalog, kbErr := kb.Scan(cfg.KB.Root, kb.Options{ShowWorkspace: cfg.KB.ShowWorkspace})
	if kbErr != nil {
		logger.Error("知识库扫描失败", "root", cfg.KB.Root, "err", kbErr)
		fmt.Fprintf(os.Stderr, "\n✗ 无法读取知识库：%v\n"+
			"  请检查 [kb] root（或 -kb 参数）是否指向存放 .md 的目录。\n", kbErr)
		return 2
	}
	logger.Info("知识库已加载",
		"root", catalog.KBRoot,
		"entries", catalog.Total,
		"categories", len(catalog.Categories),
		"root_files", len(catalog.RootFiles),
		"version", catalog.Version,
	)
	renderer := render.New(catalog, render.Options{})

	// S4：状态库与会话密钥（状态库在 KB 之外，绝不写进知识库）
	store, storeErr := state.Open(mustStatePath())
	if storeErr != nil {
		logger.Error("状态库打开失败", "err", storeErr)
		fmt.Fprintf(os.Stderr, "\n✗ 无法打开状态库：%v\n", storeErr)
		return 2
	}
	defer func() { _ = store.Close() }()

	session, sessionErr := ensureSession(store, cfg)
	if sessionErr != nil {
		logger.Error("会话密钥不可用", "err", sessionErr)
		fmt.Fprintf(os.Stderr, "\n✗ 无法初始化会话密钥：%v\n", sessionErr)
		return 2
	}
	limiter := auth.NewLimiter(cfg.Security.LoginMaxAttempts,
		time.Duration(cfg.Security.LoginWindowS)*time.Second,
		time.Duration(cfg.Security.LoginLockS)*time.Second)

	pwState, pwInitErr := ensurePassword(store, cfg, logger)
	if pwInitErr != nil {
		logger.Warn("初始口令设置失败", "err", pwInitErr)
	}

	// S5：HTTP 服务器与多地址监听
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	mgr := server.NewManager(cfg.Server.Listen, time.Duration(cfg.Server.RebindIntervalS)*time.Second, srv, logger)

	app, appErr := httpapi.New(httpapi.Options{
		Cfg:       cfg,
		Log:       logger,
		Listen:    mgr,
		KB:        catalog,
		Renderer:  renderer,
		Store:     store,
		Session:   session,
		Limiter:   limiter,
		ScannedAt: scannedAt,
		Dev:       *flagDev,
	})
	if appErr != nil {
		logger.Error("初始化 HTTP 层失败", "err", appErr)
		return 2
	}
	srv.Handler = app.Handler()

	if err := mgr.Start(); err != nil {
		logger.Error("监听失败", "err", err)
		fmt.Fprintf(os.Stderr, "\n✗ 端口被占用或地址不可用：%v\n"+
			"  可用 `readtool doctor` 查看端口占用与网卡地址。\n", err)
		return 1
	}

	mgr.PrintSummary(os.Stdout)
	fmt.Printf("知识库：%s（%d 条，%d 个分类）\n", catalog.KBRoot, catalog.Total, len(catalog.Categories))
	fmt.Printf("状态库：%s\n", store.Path())
	switch pwState {
	case passwordInitialized:
		fmt.Println("访问口令：已按配置的 init_password 初始化")
	case passwordMissing:
		fmt.Println("访问口令：尚未设置 —— 用平板打开上面的地址，按页面提示设置口令")
	}
	logger.Info("started", "listeners", mgr.Status())

	// S8：等待信号，按序关停
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("HTTP 关停超时", "err", err)
	}
	if err := mgr.Close(); err != nil {
		logger.Warn("监听关闭异常", "err", err)
	}
	logger.Info("stopped")
	return 0
}

func orNone(s string) string {
	if s == "" {
		return "(内置默认)"
	}
	return s
}
