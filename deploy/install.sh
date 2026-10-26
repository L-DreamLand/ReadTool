#!/bin/sh
# ReadTool Linux 安装脚本：建系统用户、装二进制、写配置、装 systemd unit、开机自启。
#
# 用法：
#   sudo ./deploy/install.sh --kb /srv/knowledge-base
#   sudo ./deploy/install.sh --kb /home/me/kb --listen 192.168.1.10:8123,10.144.144.5:8123
#   sudo ./deploy/install.sh --uninstall
#
# 依赖：systemd（v235+，Debian 10 / Ubuntu 20.04 / CentOS 8 及以上）、root 权限。
# 只做可重复执行的事：用户、目录、unit 都按"存在就跳过/覆盖"处理。

set -eu

SERVICE=readtool
SVC_USER=readtool
BIN=/usr/local/bin/readtool
CONF_DIR=/etc/readtool
CONF=$CONF_DIR/config.toml
UNIT=/etc/systemd/system/$SERVICE.service
STATE_DIR=/var/lib/readtool
SRC_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

KB=""
LISTEN=""
PORT=""
DO_UNINSTALL=0

die() { printf '✗ %s\n' "$*" >&2; exit 1; }
say() { printf '%s\n' "$*"; }

usage() {
	cat <<'EOF'
用法：sudo ./deploy/install.sh [选项]

  --kb <目录>        知识库根目录（必填，除非已经装好过）
  --listen <列表>    监听地址，逗号分隔，例如 "192.168.1.10:8123,10.144.144.5:8123"
                     不传则沿用 config.toml 里的值（首次生成时是 0.0.0.0:8123）
  --port <端口>      端口（仅在 listen 未指定、且要改默认 8123 时用）
  --binary <路径>    预编译好的 linux 二进制（默认找仓库根目录的 readtool 或 readtool-linux-*）
  --uninstall        卸载：停服务、删 unit、删 /etc/readtool（保留状态库与知识库）
  -h, --help         显示本帮助

交叉编译（在 Windows 或别的机器上）：
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o readtool-linux-arm64 ./cmd/readtool
EOF
}

BIN_SRC=""
while [ $# -gt 0 ]; do
	case "$1" in
	--kb) [ $# -ge 2 ] || die "--kb 需要参数"; KB=$2; shift 2 ;;
	--listen) [ $# -ge 2 ] || die "--listen 需要参数"; LISTEN=$2; shift 2 ;;
	--port) [ $# -ge 2 ] || die "--port 需要参数"; PORT=$2; shift 2 ;;
	--binary) [ $# -ge 2 ] || die "--binary 需要参数"; BIN_SRC=$2; shift 2 ;;
	--uninstall) DO_UNINSTALL=1; shift ;;
	-h|--help) usage; exit 0 ;;
	*) die "未知参数：$1（用 --help 看用法）" ;;
	esac
done

# 参数层面的错误一律在"动任何东西之前"报掉：否则用户会看到建了用户、装了二进制、
# 生成了配置，最后才因为 listen 写错而失败，状态半新半旧。
if [ -n "$LISTEN" ]; then
	case ",$LISTEN," in
	*,0.0.0.0:*,*) die "--listen 里不能同时有 0.0.0.0 与具体地址：二选一" ;;
	*,::,*) die "--listen 里不能同时有 :: 与具体地址：二选一" ;;
	esac
	case "$LISTEN" in
	*,) die "--listen 结尾多了逗号" ;;
	esac
fi
case "$PORT" in
''|*[!0-9]*) [ -z "$PORT" ] || die "--port 必须是数字" ;;
esac

[ "$(id -u)" -eq 0 ] || die "需要 root：sudo $0 ..."
command -v systemctl >/dev/null 2>&1 || die "没有 systemctl，本脚本只支持 systemd 系统"

if [ "$DO_UNINSTALL" -eq 1 ]; then
	say "==> 停止并禁用服务"
	systemctl disable --now "$SERVICE" 2>/dev/null || true
	rm -f "$UNIT"
	systemctl daemon-reload
	rm -rf "$CONF_DIR"
	say "✓ 已卸载。以下内容**保留**（要删自己删）："
	say "    状态库与日志：$STATE_DIR"
	say "    程序：$BIN"
	say "    用户：$SVC_USER（userdel $SVC_USER 可删）"
	exit 0
fi

# ---------- 1. 知识库 ----------
if [ -z "$KB" ] && [ -f "$CONF" ]; then
	KB=$(sed -n 's/^root *= *"\(.*\)"/\1/p' "$CONF" | head -1)
	[ -n "$KB" ] && say "==> 沿用已有配置里的知识库：$KB"
fi
[ -n "$KB" ] || die "缺少 --kb：请指定知识库根目录（例如 --kb /srv/knowledge-base）"
[ -d "$KB" ] || die "知识库目录不存在：$KB"
KB=$(CDPATH= cd -- "$KB" && pwd)
# 去掉结尾斜杠，unit 里好看一点
KB=${KB%/}

# ---------- 2. 二进制 ----------
if [ -z "$BIN_SRC" ]; then
	for cand in "$SRC_DIR/readtool" "$SRC_DIR/readtool-linux-amd64" "$SRC_DIR/readtool-linux-arm64" "$SRC_DIR/readtool.exe"; do
		[ -f "$cand" ] && BIN_SRC=$cand && break
	done
fi
[ -n "$BIN_SRC" ] || die "找不到二进制：请用 --binary 指定，或先在源码目录 go build"
[ -f "$BIN_SRC" ] || die "二进制不存在：$BIN_SRC"
if ! head -c 4 "$BIN_SRC" 2>/dev/null | od -An -tx1 | grep -q '7f 45 4c 46'; then
	die "$BIN_SRC 不是 Linux ELF 可执行文件（可能是 Windows 的 .exe）。请交叉编译：
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags \"-s -w\" -o readtool-linux-amd64 ./cmd/readtool"
fi

# ---------- 3. 系统用户 ----------
if id "$SVC_USER" >/dev/null 2>&1; then
	say "==> 用户 $SVC_USER 已存在，跳过创建"
else
	say "==> 创建系统用户 $SVC_USER（无登录 shell、无家目录）"
	useradd --system --user-group --no-create-home --home-dir "$STATE_DIR" \
		--shell /usr/sbin/nologin "$SVC_USER" 2>/dev/null ||
		useradd --system --no-create-home --home-dir "$STATE_DIR" --shell /sbin/nologin "$SVC_USER"
fi

# ---------- 4. 安装二进制 ----------
say "==> 安装二进制到 $BIN"
install -m 0755 "$BIN_SRC" "$BIN"

# ---------- 5. 状态目录 ----------
# StateDirectory= 本来会建，但第一次生成配置要在这里落地，先手工建一次更稳妥。
install -d -m 0750 -o "$SVC_USER" -g "$SVC_USER" "$STATE_DIR"

# ---------- 6. 配置文件 ----------
install -d -m 0755 "$CONF_DIR"
if [ -f "$CONF" ]; then
	say "==> 已存在 $CONF（不覆盖；要重建请先备份再删除）"
	echo "# 由 install.sh 于 $(date '+%Y-%m-%d %H:%M:%S') 更新" >/dev/null
else
	say "==> 生成默认配置 $CONF"
	"$BIN" -print-default-config -kb "$KB" >"$CONF"
fi

# 写入/替换 listen（只在显式给了 --listen 时；否则保留模板里的 0.0.0.0:8123）
if [ -n "$LISTEN" ]; then
	# "a:1,b:2" → TOML 数组 ["a:1", "b:2"]（合法性已在参数解析处校验过）
	arr=""
	oldifs=$IFS
	IFS=,
	for one in $LISTEN; do
		[ -n "$one" ] || continue
		arr="$arr\"$one\", "
	done
	IFS=$oldifs
	arr=$(printf '%s' "$arr" | sed 's/, $//')
	sed -i "s|^listen *=.*|listen = [$arr]|" "$CONF"
	say "==> 监听地址设为 [$arr]"
fi
if [ -n "$PORT" ]; then
	sed -i "s|^port *=.*|port = $PORT|" "$CONF"
fi

chown root:"$SVC_USER" "$CONF"
chmod 0640 "$CONF"

# ---------- 7. 权限自查 ----------
can_read() {
	if command -v runuser >/dev/null 2>&1; then
		runuser -u "$SVC_USER" -- test -r "$1" && runuser -u "$SVC_USER" -- test -x "$1"
	else
		su -s /bin/sh -c "test -r '$1' && test -x '$1'" "$SVC_USER"
	fi
}
if ! can_read "$KB"; then
	say ""
	say "⚠ $SVC_USER 读不到 $KB。常见原因：目录权限是 700 或属主是你自己。"
	say "  二选一："
	say "    sudo setfacl -R -m u:$SVC_USER:rX -m d:u:$SVC_USER:rX '$KB'   # 保留原权限，只加读"
	say "    sudo chmod -R a+rX '$KB'                                      # 简单粗暴，所有人可读"
fi

# ---------- 8. 文档 ----------
if [ -f "$SRC_DIR/README.md" ]; then
	install -d -m 0755 /usr/local/share/doc/readtool
	install -m 0644 "$SRC_DIR/README.md" /usr/local/share/doc/readtool/README.md
fi

# ---------- 9. 安装 unit ----------
say "==> 安装 systemd unit 到 $UNIT"
sed "s|@KB_ROOT@|$KB|" "$SRC_DIR/deploy/readtool.service" >"$UNIT"
chmod 0644 "$UNIT"
systemctl daemon-reload
systemctl enable "$SERVICE" >/dev/null 2>&1 || true
systemctl restart "$SERVICE"

# ---------- 10. 汇报 ----------
sleep 1
say ""
if systemctl is-active --quiet "$SERVICE"; then
	say "✓ $SERVICE 已启动，并已设为开机自启"
else
	say "✗ $SERVICE 没能起来，看日志："
	say "    journalctl -u $SERVICE -n 50 --no-pager"
	exit 1
fi
say ""
say "可访问地址（在平板上填其中一个，首次访问会引导你设访问口令）："
listen_line=$("$BIN" doctor -config "$CONF" 2>/dev/null | sed -n 's/^ *listen *= *//p' | head -1)
[ -n "$listen_line" ] && say "    listen = $listen_line"
say "    （这是 doctor 的建议值；想用它就编辑上面的配置再 restart）"
say ""
say "常用命令："
say "    systemctl status $SERVICE                # 状态"
say "    journalctl -u $SERVICE -f                # 跟日志"
say "    systemctl restart $SERVICE               # 重启"
say "    sudoedit $CONF && sudo systemctl restart $SERVICE   # 改配置"
say ""
say "状态库与日志：$STATE_DIR（$SERVICE 用户所有）"
