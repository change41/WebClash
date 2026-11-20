#!/bin/sh
# 数据兼容性 parity：用生产 data 副本跑 Go 二进制，确认读取既有磁盘数据并正常运行。
# 用法: sh parity.sh <binary>   （数据目录挂载于 /data，含 settings.json/profiles/bin/mihomo）
set -eu

BIN=${1:-/pkg/webclash}
PORT=7799
BASE="http://127.0.0.1:$PORT"
W=$(mktemp -d)
LOG="$W/server.log"
JAR="$W/cookies"
FAILS=0
PID=""

cleanup() {
  if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
    kill -TERM "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  rm -rf "$W"
}
trap cleanup EXIT

ok() { printf '  ok   %s\n' "$1"; }
bad() { printf '  FAIL %s\n' "$1"; FAILS=$((FAILS + 1)); }
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

echo "== 1. version --json =="
VER=$("$BIN" version --json)
case "$VER" in *'"static": true'*) ok "static=true" ;; *) bad "static" ;; esac
case "$VER" in *'"embedded": true'*) ok "embedded=true" ;; *) bad "embedded" ;; esac
case "$VER" in *'"runtime": "go'*) ok "runtime=go" ;; *) bad "runtime" ;; esac

echo "== 2. 读取既有 settings/profiles 启动 =="
[ -f /data/settings.json ] || { echo "缺少 /data/settings.json"; exit 1; }
[ -f /data/initial-password.txt ] || { echo "缺少 /data/initial-password.txt"; exit 1; }
PASS=$(cat /data/initial-password.txt)
"$BIN" run -p "$PORT" -H 127.0.0.1 -d /data >"$LOG" 2>&1 &
PID=$!
i=0
until curl -sf "$BASE/api/health" >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -gt 120 ]; then echo "health 超时"; cat "$LOG"; exit 1; fi
  sleep 0.5
done
ok "health 就绪"

echo "== 3. 既有密码登录 =="
LOGIN=$(curl -s -c "$JAR" -H 'Content-Type: application/json' -d "{\"password\":\"$PASS\"}" "$BASE/api/auth/login")
case "$LOGIN" in *'"ok":true'*) ok "初始密码登录（scrypt 哈希兼容）" ;; *) bad "登录失败: $LOGIN" ;; esac

echo "== 4. 既有订阅加载 =="
PROF=$(curl -s -b "$JAR" "$BASE/api/profiles")
for id in $(ls /data/profiles | sed 's/\.yaml$//'); do
  case "$PROF" in *"$id"*) ok "订阅 $id 已加载" ;; *) bad "订阅 $id 缺失" ;; esac
done

echo "== 5. settings 兼容 =="
SET=$(curl -s -b "$JAR" "$BASE/api/settings")
if printf '%s' "$SET" | grep -q '"hasPassword": *true'; then ok "settings hasPassword=true"; else bad "settings hasPassword"; fi

echo "== 6. 既有内核 autoStart =="
i=0
STATE=""
until [ "$STATE" = "running" ]; do
  i=$((i + 1))
  if [ "$i" -gt 180 ]; then bad "内核启动超时"; cat "$LOG"; break; fi
  sleep 1
  STATE=$(curl -s -b "$JAR" "$BASE/api/kernel/status" | grep -o '"state":"[a-z]*"' | head -1 | cut -d'"' -f4)
done
[ "$STATE" = "running" ] && ok "内核 running（复用既有 bin/mihomo）"

echo "== 7. clash 透传 =="
check_code() { if [ "$(code -b "$JAR" "$BASE$1")" = "200" ]; then ok "$1 200"; else bad "$1 非 200"; fi; }
check_code /api/clash/version
check_code /api/clash/configs

echo "== 8. 静态前端 =="
if [ "$(code "$BASE/")" = "200" ]; then ok "GET / 200"; else bad "GET /"; fi

echo "== 9. 优雅退出 + 内核回收 =="
kill -TERM "$PID"
wait "$PID" 2>/dev/null && EXIT=0 || EXIT=$?
if [ "$EXIT" = "0" ]; then ok "SIGTERM exit0"; else bad "SIGTERM exit $EXIT"; fi
if grep -q "收到 SIGTERM" "$LOG"; then ok "优雅退出日志"; else bad "缺少退出日志"; fi
LEFT=0
for c in /proc/[0-9]*/comm; do
  [ -r "$c" ] || continue
  read -r name <"$c" 2>/dev/null || continue
  [ "$name" = "mihomo" ] && LEFT=$((LEFT + 1))
done
if [ "$LEFT" = "0" ]; then ok "mihomo 已回收"; else bad "mihomo 残留 $LEFT"; fi
PID=""

echo
if [ "$FAILS" -gt 0 ]; then
  echo "parity 失败：$FAILS 项"
  echo "--- server.log ---"
  cat "$LOG" 2>/dev/null || true
  exit 1
fi
echo "parity 全部通过"