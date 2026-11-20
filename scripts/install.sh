#!/bin/sh
# WebClash 一键安装（免 Node）。POSIX sh，自动检测架构 / 服务管理器。
# Linux 产物为 CGO_ENABLED=0 静态二进制，glibc 与 musl 通用，无需 Node/libstdc++。
# macOS 产物为原生 darwin 二进制（仅安装二进制，无服务）。
#
# 离线:  sh install.sh --local ./webclash-linux-x64.tar.xz
# 在线:  sh install.sh --base-url https://github.com/OWNER/REPO/releases/download/v0.1.0 --version v0.1.0
# 仅二进制（非 root）: sh install.sh --local ./webclash-linux-x64 --dir ~/.local/bin --no-service
# macOS: sh install.sh --local ./webclash-darwin-arm64.tar.gz --dir ~/.local/bin --no-service
set -eu

DIR=/usr/local/bin
DATA_DIR=/var/lib/webclash
PORT=7788
HOST=0.0.0.0
LOCAL=""
BASE_URL="${WEBCLASH_BASE_URL:-}"
VERSION="${WEBCLASH_VERSION:-}"
MIRROR=""
SERVICE="auto"
DRY_RUN=0
ASSUME_YES=0

die() { printf '错误: %s\n' "$1" >&2; exit 1; }
info() { printf '%s\n' "$1"; }
need_cmd() { command -v "$1" >/dev/null 2>&1 || die "缺少命令: $1"; }

usage() {
  cat <<'EOF'
用法: sh install.sh [选项]
  --local <文件>        使用本地产物（离线安装，优先）
  --base-url <URL>      产物下载目录（含 webclash-linux-* 与 SHA256SUMS）
  --version <TAG>       发布版本（在线模式必填，仅用于提示）
  --mirror <前缀>       GitHub 加速前缀（如 https://ghfast.top/）
  --dir <目录>          二进制安装目录（默认 /usr/local/bin）
  --data-dir <目录>     服务数据目录（默认 /var/lib/webclash）
  --port <端口>         WebUI 端口（默认 7788）
  --host <地址>         监听地址（默认 0.0.0.0）
  --service             强制安装为系统服务
  --no-service          只安装二进制，不安装服务
  --dry-run             只打印检测与将执行的动作
  -y, --yes             跳过交互确认
  -h, --help            显示帮助
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --local) LOCAL=${2:-}; shift 2 ;;
    --base-url) BASE_URL=${2:-}; shift 2 ;;
    --version) VERSION=${2:-}; shift 2 ;;
    --mirror) MIRROR=${2:-}; shift 2 ;;
    --dir) DIR=${2:-}; shift 2 ;;
    --data-dir) DATA_DIR=${2:-}; shift 2 ;;
    --port) PORT=${2:-}; shift 2 ;;
    --host) HOST=${2:-}; shift 2 ;;
    --service) SERVICE="yes"; shift ;;
    --no-service) SERVICE="no"; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -y|--yes) ASSUME_YES=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "未知参数: $1（--help 查看用法）" ;;
  esac
done

# ---- 系统检测 ----
OS=$(uname -s)
case "$OS" in
  Linux) OSNAME=linux ;;
  Darwin) OSNAME=darwin ;;
  *) die "本脚本仅支持 Linux/macOS（当前 $OS）。Windows 请使用 Docker 部署。" ;;
esac

case "$(uname -m)" in
  x86_64|amd64) ARCH=x64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "不支持的架构: $(uname -m)（支持 x86_64 / aarch64）。其它架构请用 Docker 或源码部署。" ;;
esac

ARTIFACT="webclash-${OSNAME}-${ARCH}"
if [ "$OSNAME" = "darwin" ]; then
  info "检测到: darwin/$ARCH（原生 macOS 二进制，无需 Node）"
else
  info "检测到: linux/$ARCH（静态二进制，glibc/musl 通用，无需 Node）"
fi

version_ge() {
  # version_ge A B → A >= B ?
  a_major=${1%%.*}; a_rest=${1#*.}; a_minor=${a_rest%%.*}
  b_major=${2%%.*}; b_rest=${2#*.}; b_minor=${b_rest%%.*}
  [ "$a_major" -gt "$b_major" ] && return 0
  [ "$a_major" -lt "$b_major" ] && return 1
  [ "$a_minor" -ge "$b_minor" ]
}

if [ "$OSNAME" = "linux" ]; then
  KVER=$(uname -r | grep -o '^[0-9][0-9]*\.[0-9][0-9]*') || KVER=""
  if [ -n "$KVER" ] && ! version_ge "$KVER" "4.18"; then
    info "警告: 内核 $KVER 较旧（建议 >= 4.18），可能出现兼容性问题。"
  fi
fi

MANAGER=none
if [ "$OSNAME" = "linux" ]; then
  if [ -d /run/systemd/system ] || [ "$(cat /proc/1/comm 2>/dev/null)" = "systemd" ]; then
    MANAGER=systemd
  elif [ -x /sbin/openrc ] || [ -e /run/openrc/softlevel ] || command -v rc-service >/dev/null 2>&1; then
    MANAGER=openrc
  fi
fi
info "服务管理器: $MANAGER"

if [ "$OSNAME" = "darwin" ] && [ "$SERVICE" = "yes" ]; then
  die "macOS 暂不支持服务安装，请前台运行（--no-service）。"
fi

# ---- 取产物 ----
need_cmd curl
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
PAYLOAD=""

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}

if [ -n "$LOCAL" ]; then
  [ -f "$LOCAL" ] || die "本地文件不存在: $LOCAL"
  PAYLOAD=$LOCAL
  info "使用本地产物: $LOCAL"
else
  [ -n "$BASE_URL" ] || die "在线安装需 --base-url <产物目录URL>（或用 --local 离线安装）。"
  BASE_URL=${BASE_URL%/}
  FILE="$ARTIFACT"
  if [ "$OSNAME" = "darwin" ]; then
    FILE="$ARTIFACT.tar.gz"
  elif command -v xz >/dev/null 2>&1; then
    FILE="$ARTIFACT.tar.xz"
  fi
  URL="$BASE_URL/$FILE"
  case "$URL" in
    https://github.com/*)
      PREFIXES="${MIRROR:+$MIRROR }https://ghfast.top/ https://gh-proxy.com/ https://ghproxy.net/ "
      ;;
    *) PREFIXES="" ;;
  esac
  OK=0
  for prefix in $PREFIXES ""; do
    TRY="${prefix}${URL}"
    info "下载 $TRY"
    if curl -fsSL --connect-timeout 15 -o "$TMP/$FILE" "$TRY"; then OK=1; break; fi
  done
  [ "$OK" -eq 1 ] || die "下载失败: $URL"
  if curl -fsSL --connect-timeout 15 -o "$TMP/SHA256SUMS" "$BASE_URL/SHA256SUMS" 2>/dev/null; then
    EXPECT=$(grep "  $FILE\$" "$TMP/SHA256SUMS" | awk '{print $1}') || EXPECT=""
    if [ -n "$EXPECT" ]; then
      ACTUAL=$(sha256_of "$TMP/$FILE")
      [ "$EXPECT" = "$ACTUAL" ] || { rm -f "$TMP/$FILE"; die "sha256 校验失败（期望 $EXPECT，实际 $ACTUAL）"; }
      info "sha256 校验通过"
    fi
  else
    info "警告: 无法获取 SHA256SUMS，跳过校验"
  fi
  PAYLOAD="$TMP/$FILE"
fi

case "$PAYLOAD" in
  *.tar.xz)
    need_cmd xz
    [ "$DRY_RUN" -eq 1 ] || tar -xJf "$PAYLOAD" -C "$TMP"
    BIN_SRC="$TMP/$ARTIFACT"
    ;;
  *.tar.gz)
    [ "$DRY_RUN" -eq 1 ] || tar -xzf "$PAYLOAD" -C "$TMP"
    BIN_SRC="$TMP/$ARTIFACT"
    ;;
  *) BIN_SRC=$PAYLOAD ;;
esac

# ---- 安装二进制 ----
case "$DIR" in
  "~") DIR=$HOME ;;
  "~/"*) DIR="$HOME/${DIR#~/}" ;;
esac
BIN="$DIR/webclash"

if [ "$(id -u)" != "0" ]; then
  if [ "$SERVICE" = "yes" ]; then die "安装服务需要 root，请使用 sudo。"; fi
  case "$DIR" in
    /usr/*|/bin*|/sbin*) die "非 root 无法写入 $DIR，请使用 sudo 或 --dir ~/.local/bin --no-service。" ;;
  esac
fi

INSTALL_SERVICE=0
if [ "$SERVICE" = "yes" ]; then
  [ "$MANAGER" != "none" ] || die "未检测到 systemd/OpenRC，无法安装服务（可用 --no-service 仅安装二进制）。"
  INSTALL_SERVICE=1
elif [ "$SERVICE" = "auto" ] && [ "$(id -u)" = "0" ] && [ "$MANAGER" != "none" ]; then
  INSTALL_SERVICE=1
fi

if [ "$DRY_RUN" -eq 1 ]; then
  info "[dry-run] mkdir -p $DIR"
  info "[dry-run] cp $BIN_SRC $BIN.new && chmod 0755 $BIN.new && mv -f $BIN.new $BIN"
  if [ "$INSTALL_SERVICE" -eq 1 ]; then
    info "[dry-run] $BIN install --data-dir $DATA_DIR --port $PORT --host $HOST --service-manager $MANAGER"
  fi
  exit 0
fi

mkdir -p "$DIR"
cp "$BIN_SRC" "$BIN.new"
chmod 0755 "$BIN.new"
mv -f "$BIN.new" "$BIN"
info "已安装: $BIN"

if [ "$INSTALL_SERVICE" -eq 1 ]; then
  "$BIN" install --data-dir "$DATA_DIR" --port "$PORT" --host "$HOST" --service-manager "$MANAGER"
else
  info "未安装服务。前台运行: $BIN run -d $DATA_DIR -p $PORT"
fi

cat <<EOF

============================================================
 WebClash 安装完成
 访问:      http://<主机地址>:$PORT
 管理密码:  $BIN password --data-dir $DATA_DIR
            （或 cat $DATA_DIR/initial-password.txt）
 数据目录:  $DATA_DIR
EOF
if [ "$INSTALL_SERVICE" -eq 1 ]; then
  cat <<EOF
 常用命令:  $BIN status | start | stop | restart
 日志:      $([ "$MANAGER" = "systemd" ] && echo "journalctl -u webclash -f" || echo "tail -f /var/log/webclash.log")
 TUN 模式:  需 /dev/net/tun 与 NET_ADMIN 权限（systemd 已配置）
EOF
else
  cat <<EOF
 前台运行:  $BIN run -d $DATA_DIR -p $PORT
EOF
fi
cat <<EOF
============================================================
EOF