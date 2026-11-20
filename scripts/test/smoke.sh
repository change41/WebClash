#!/bin/sh
# 容器内冒烟：验证免 Node 单文件二进制的完整行为。
# 用法: sh smoke.sh <binary> [port]
set -eu

BIN=${1:-/pkg/webclash}
PORT=${2:-7799}
PASS="smoke-pass-123"
BASE="http://127.0.0.1:$PORT"
WORK=$(mktemp -d)
LOG="$WORK/server.log"
JAR="$WORK/cookies"
FAILS=0
PID=""

cleanup() {
  if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
    kill -TERM "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

ok() { printf '  ok   %s\n' "$1"; }
bad() { printf '  FAIL %s\n' "$1"; FAILS=$((FAILS + 1)); }
check() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1（期望 $3，实际 $2）"; fi; }
contains() { case "$2" in *"$3"*) ok "$1" ;; *) bad "$1（缺少 $3）" ;; esac; }

code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

echo "== 1. 环境断言：无 node =="
if command -v node >/dev/null 2>&1; then bad "PATH 中存在 node"; else ok "无 node"; fi
[ -x "$BIN" ] || { echo "二进制不可执行: $BIN"; exit 1; }

echo "== 2. version --json =="
VER=$("$BIN" version --json)
contains "embedded=true" "$VER" '"embedded": true'
contains "static=true" "$VER" '"static": true'
contains "runtime=go" "$VER" '"runtime": "go'

echo "== 3. run + health =="
"$BIN" run -p "$PORT" -H 127.0.0.1 -d "$WORK/data" --password "$PASS" >"$LOG" 2>&1 &
PID=$!
i=0
until curl -sf "$BASE/api/health" >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -gt 60 ]; then echo "health 超时"; cat "$LOG"; exit 1; fi
  sleep 0.5
done
ok "health 就绪"

echo "== 4. 登录 + auth status =="
LOGIN=$(curl -s -c "$JAR" -H 'Content-Type: application/json' -d "{\"password\":\"$PASS\"}" "$BASE/api/auth/login")
contains "login ok" "$LOGIN" '"ok":true'
STATUS=$(curl -s -b "$JAR" "$BASE/api/auth/status")
contains "authenticated" "$STATUS" '"authenticated":true'

echo "== 5. 401 边界 =="
check "未登录 /api/settings" "$(code "$BASE/api/settings")" "401"

echo "== 6. 静态资源 + 缓存头 =="
check "GET /" "$(code "$BASE/")" "200"
curl -s "$BASE/" >"$WORK/index.html"
ASSET=$(grep -o 'assets/index-[^"]*\.js' "$WORK/index.html" | head -1)
[ -n "$ASSET" ] || { bad "index.html 未引用 assets"; ASSET="assets/missing.js"; }
HDR=$(curl -sI "$BASE/$ASSET")
contains "immutable 缓存" "$HDR" "immutable"
ETAG=$(printf '%s' "$HDR" | tr -d '\r' | awk -F': ' 'tolower($1)=="etag"{print $2}')
[ -n "$ETAG" ] && ok "存在 ETag" || bad "缺少 ETag"

echo "== 7. 304 协商 =="
check "If-None-Match" "$(code -H "If-None-Match: $ETAG" "$BASE/$ASSET")" "304"

echo "== 8. SPA 回落 + /api 404 =="
check "SPA /proxies" "$(code "$BASE/proxies")" "200"
check "GET /api/nope" "$(code -b "$JAR" "$BASE/api/nope")" "404"
check "POST /api/nope" "$(code -b "$JAR" -X POST "$BASE/api/nope")" "404"

echo "== 9. 路径穿越 =="
T1=$(code --path-as-is "$BASE/assets/../index.html")
case "$T1" in 200|400|404) ok "穿越归一化 ($T1)" ;; *) bad "穿越异常 ($T1)" ;; esac
T2=$(code --path-as-is "$BASE/..%2f..%2fetc/passwd")
case "$T2" in 200|400|404) ok "编码穿越拒绝 ($T2)" ;; *) bad "编码穿越异常 ($T2)" ;; esac

echo "== 10. WebSocket 鉴权 =="
WS_HDR='-H Connection:Upgrade -H Upgrade:websocket -H Sec-WebSocket-Version:13 -H Sec-WebSocket-Key:dGhlIHNhbXBsZSBub25jZQ=='
check "WS 无 cookie" "$(code $WS_HDR "$BASE/ws/traffic")" "401"
check "WS 有 cookie" "$(code $WS_HDR -b "$JAR" "$BASE/ws/traffic")" "101"

echo "== 11. HEAD =="
check "HEAD /" "$(code -I "$BASE/")" "200"

echo "== 12. 内核下载启动 + 优雅退出 =="
curl -s -b "$JAR" -X POST "$BASE/api/kernel/start" >/dev/null
i=0
STATE=""
until [ "$STATE" = "running" ]; do
  i=$((i + 1))
  if [ "$i" -gt 360 ]; then bad "内核启动超时"; break; fi
  sleep 1
  STATE=$(curl -s -b "$JAR" "$BASE/api/kernel/status" | grep -o '"state":"[a-z]*"' | head -1 | cut -d'"' -f4)
done
[ "$STATE" = "running" ] && ok "内核 running"
MIHOMO_PID=$(curl -s -b "$JAR" "$BASE/api/kernel/status" | grep -o '"pid":[0-9]*' | head -1 | cut -d: -f2)

kill -TERM "$PID"
wait "$PID" 2>/dev/null && EXIT=0 || EXIT=$?
check "SIGTERM 退出码" "$EXIT" "0"
contains "优雅退出日志" "$(cat "$LOG")" "收到 SIGTERM"
LEFT=0
for c in /proc/[0-9]*/comm; do
  [ -r "$c" ] || continue
  read -r name <"$c" 2>/dev/null || continue
  [ "$name" = "mihomo" ] && LEFT=$((LEFT + 1))
done
check "mihomo 已回收" "$LEFT" "0"
PID=""

echo
if [ "$FAILS" -gt 0 ]; then
  echo "冒烟失败：$FAILS 项"
  echo "--- server.log ---"
  cat "$LOG" 2>/dev/null || true
  exit 1
fi
echo "冒烟全部通过"