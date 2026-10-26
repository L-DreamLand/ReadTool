# ReadTool

把本地的**文件型知识库**（一堆 Markdown 目录）通过局域网或虚拟局域网，给平板 / 手机上的浏览器读的**只读**阅读服务。

一个静态单文件二进制（Go，`CGO_ENABLED=0`）跑在你自己的机器上：服务端把 Markdown 渲染成 HTML、建好中文全文索引；
平板打开浏览器就能读，还能记住读到哪儿、收藏章节。**知识库严格只读**，阅读状态单独存在状态库里，
不往知识库里写一个字节。

```
平板 / 手机浏览器  ──局域网或虚拟局域网──▶  readtool（单进程）  ──只读──▶  Knowledge-Base/*.md
                                              │
                                              └──▶ state.db（口令、书签、进度）
```

## 特性

| 能力 | 说明 |
|---|---|
| 目录浏览 | 按分类分组的条目列表，自动读 `index.json` / 首个 `# 标题` 作为显示名 |
| Markdown 渲染 | 服务端渲染（goldmark + GFM），标题锚点、相对链接互跳、坏链标记 |
| 宽表三档 | 按**单元格实测宽度**自动切换：表格 / 横向滚动+冻结首列 / 卡片式，竖屏不裁列 |
| 全文搜索 | 中文按二字切分建内存倒排，标题×3 / 关键词×2 / 摘要×1.5 / 正文×1 加权，命中高亮 |
| 阅读状态 | 书签、阅读进度跨设备同步（4 秒节流），「最近阅读」 |
| 代码块 | chroma 语法高亮 + 语言标签 + 一键复制 |
| 排版设置 | 字号 / 行距 / 主题（含护眼米黄）/ 宋体，本地记住 |
| 手动重扫 | 顶栏 `⟳`：往知识库加删改文件后点一下即可刷新，不必重启进程 |
| 多地址监听 | 同时绑局域网 + 虚拟局域网地址，逐个绑定、失败不静默 |
| 单口令 | 首次访问设置口令，Argon2id 哈希存储，HttpOnly Cookie 会话 |
| 装到主屏幕 | Web App Manifest + 图标，平板上有独立窗口（细节见下） |

## 快速开始

### Linux（推荐：systemd 托管、开机自启）

```sh
# 1) 编译（在任意机器上交叉编译，不需要 C 编译器）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o readtool-linux-amd64 ./cmd/readtool
#    ARM 设备（树莓派 / ARM NAS）把 GOARCH 换成 arm64

# 2) 装（会建系统用户、装二进制、写配置、装 unit、开机自启）
sudo ./deploy/install.sh --kb /srv/knowledge-base --listen 192.168.1.10:8123,10.144.144.5:8123
```

脚本可重复执行，已存在的配置不会被覆盖。它做的每一步：

| 步骤 | 落点 |
|---|---|
| 建系统用户 `readtool`（无登录 shell） | `useradd --system` |
| 装二进制 | `/usr/local/bin/readtool` |
| 生成配置 | `/etc/readtool/config.toml`（`root:readtool` `0640`） |
| 状态库 + 日志 | `/var/lib/readtool/readtool/`（`state.db`、`logs/`） |
| systemd unit | `/etc/systemd/system/readtool.service` |
| 开机自启 | `systemctl enable --now readtool` |

跑完会打印可访问地址。第一次用平板打开会引导你**设置访问口令**（也可以事先在配置里写 `init_password`）。

日常命令：

| 命令 | 作用 |
|---|---|
| `systemctl status readtool` | 看状态 |
| `journalctl -u readtool -f` | 跟日志（应用自己也会按天写 `/var/lib/readtool/readtool/logs/`） |
| `sudo systemctl restart readtool` | 改完配置后重启（配置**没有**热重载） |
| `sudo ./deploy/install.sh --uninstall` | 停服务、删 unit 与 `/etc/readtool`（状态库和知识库不动） |

### Windows（直接跑二进制）

`readtool.exe` 是**控制台服务程序**，不是窗口程序——没有界面，启动后就在后台等浏览器来连。
所以**不要双击它**：端口已被占用（比如自己已经跑着一份）时它会打印报错后立刻退出，窗口一闪就关，
看起来像"双击没反应"。

```powershell
go build -trimpath -ldflags "-s -w" -o readtool.exe ./cmd/readtool
.\readtool.exe -kb "D:\Knowledge-Base" -listen 192.168.1.10:8123,10.144.144.5:8123
```

想要"双击启动、已在运行就只显示状态、出错时窗口停住"，用任意方式给自己包一层（计划任务 / 快捷方式 + `cmd /k`）。

### 第一次用

1. 平板与电脑在同一 Wi-Fi（或都加入同一个虚拟局域网，如 EasyTier / Tailscale）。
2. 浏览器打开 `http://<电脑的监听地址>:8123`，例如 `http://192.168.1.10:8123`。
3. 按提示**设置访问口令**（至少 8 位），之后这台设备自动记住。

## 配置

首次启动会在用户配置目录生成一份带中文注释的 TOML 模板：

| 平台 | 位置 |
|---|---|
| Windows | `%LOCALAPPDATA%\ReadTool\config.toml` |
| Linux（systemd 安装） | `/etc/readtool/config.toml` |
| Linux（手动运行） | `~/.local/share/readtool/config.toml`（或 `$XDG_CONFIG_HOME`） |

要点（完整清单见 [设计规格.md](设计规格.md) §3）：

```toml
[server]
# 监听地址数组：局域网 + 虚拟局域网各一条，逐条独立绑定
listen = ["192.168.1.10:8123", "10.144.144.5:8123"]
rebind_interval_s = 30          # 失败的地址每 30 秒重试（虚拟网卡常比进程晚就绪）
# init_password = "至少8位"     # 想跳过首次引导就写这里（明文，注意文件权限）

[kb]
root = "D:/Knowledge-Base"      # 用正斜杠；反斜杠在 TOML 里是转义符
show_workspace = false          # 是否把 _workspace/ 也列出来

[ui]
default_font_px = 19
min_cell_px = 130               # 表格视图的最小单元格宽
card_view_px = 90               # 低于它就切卡片式
drawer_px = 280
```

- **禁止** `0.0.0.0` 与具体地址混写（会拒绝启动）：要么只写 `0.0.0.0:8123`，要么逐条列具体地址。
- 改 `listen` 需要重启；`ui.*` 与 `log.level` 下次打开页面生效。
- 命令行参数（`-kb` / `-listen` / `-port` / `-config`）优先级高于配置文件。

| 东西 | 位置 |
|---|---|
| 状态库 | `%LOCALAPPDATA%\ReadTool\state.db` / `/var/lib/readtool/readtool/state.db`（口令哈希、会话密钥、书签、进度；删掉等于清口令） |
| 日志 | `%LOCALAPPDATA%\ReadTool\logs\readtool-YYYY-MM-DD.log` / `/var/lib/readtool/readtool/logs/` |

## 体检与排错

```sh
readtool doctor -config /etc/readtool/config.toml   # Linux
.\readtool.exe doctor -kb "D:\Knowledge-Base"       # Windows
```

它会打印：版本、平台目录、配置文件、知识库条目数、**本机每块网卡的地址**、逐条 `listen` 能否绑定、
建议的 `listen` 片段，并按平台提示防火墙 / AP 隔离这类常见拦路虎。

常见现象对照：

| 现象 | 多半是 |
|---|---|
| 局域网能开、另一台设备打不开 | 防火墙拦入站，或路由器开了 AP 隔离（这时走虚拟局域网） |
| 服务活着但首页 0 条 | `[kb] root` 错了，或 unit 的 `ReadOnlyPaths=` 没指向知识库 |
| 启动就退出、报 `address already in use` | 已经有一份在跑；换地址或先停掉旧的 |
| 改了代码/样式，刷新没变化 | 前端是 `go:embed` 进二进制的，改完要**重新编译并重启** |
| 页面一直停在「载入中」 | 浏览器缓存了旧模块；强刷一次（`Ctrl+Shift+R`），或打开 `/diag.html` 看一眼 |

## 装到主屏幕（平板/手机）

1. 先正常打开 `http://<地址>:8123` 并输一次口令
2. **Android**：浏览器菜单 →「添加到主屏幕」/「安装应用」；**iOS Safari**：分享 →「添加到主屏幕」
3. 之后从桌面图标进入

> ⚠️ 局域网是明文 `http://`，浏览器不把它当"安全上下文"：**iOS Safari 仍会进独立窗口**
> （它看 `apple-mobile-web-app-capable`），**Android 的 Chrome/Edge 只会建一个快捷方式**，
> 点开还在标签里。想在 Android 上也进独立窗口，需要给 ReadTool 配 HTTPS 证书
> （`[security] tls_enabled = true`），自签证书得在平板上手动信任一次。

## 目录结构

```
cmd/readtool/        入口：子命令（serve / doctor / version）、启动编排
internal/config/     配置：默认值、生成模板、加载、校验
internal/platform/   平台差异：数据目录、日志文件、可执行路径
internal/kb/         知识库扫描、元数据融合、路径解析（只读边界）
internal/render/     Markdown → HTML（goldmark + chroma + 宽表包装 + 链接改写）
internal/search/     中文 bigram 倒排 + 加权打分 + 片段高亮
internal/state/      SQLite（modernc，纯 Go）：口令、会话密钥、书签、进度
internal/auth/       Argon2id 口令、HMAC 会话、登录节流
internal/server/     多地址监听、延迟重绑、状态与启动摘要
internal/httpapi/    HTTP 层：路由、中间件、鉴权、各接口、静态资源
web/                 前端（原生 ES modules，零构建，`go:embed` 进二进制）
deploy/               Linux systemd unit 模板 + 安装/卸载脚本
tools/               开发用工具（UI 三档验收、无障碍审计）
```

## 文档

| 文件 | 内容 |
|---|---|
| [软件需求设计文档.md](软件需求设计文档.md) | 做什么、验收标准（FR / NFR / UC / ADR） |
| [技术实现路径.md](技术实现路径.md) | 按什么顺序做、每一步怎么验证 |
| [设计规格.md](设计规格.md) | 软件结构、配置变量、HTTP 契约、并发模型 |
| [THIRD-PARTY.md](THIRD-PARTY.md) | 链接进二进制的第三方模块及其许可 |

## 许可

本项目以 **Apache License 2.0** 发布，全文见 [LICENSE](LICENSE)；版权与第三方声明见 [NOTICE](NOTICE)。

## 致谢

开发过程中使用了以下工具，在此按各自条款声明：

- **[billion-context](https://github.com/ranxianglei/billion-context)**（及 [acp-kernel](https://github.com/ranxianglei/acp-kernel)）——
  会话上下文压缩工具。本项目的实现过程借助它管理长会话上下文。
  依其许可的附加条款（Attribution on Product Surfaces），在此明确标注：**本项目在开发过程中使用了 billion-context
  （及 acp-kernel）**。
- **DeepSeek Harness（DSH）** —— 本地编码智能体运行环境，本项目的编码与测试在此环境中完成。

渲染与存储站在这些项目的肩膀上：goldmark、chroma、modernc.org/sqlite、BurntSushi/toml，以及 Go 标准库。
