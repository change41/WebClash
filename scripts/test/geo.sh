#!/bin/sh
# Geo 验证（修复 #3）：offline 断网降级 / online 本地镜像预置。
# 用法: sh geo.sh <binary> <seed-dir> <mode>
#   seed-dir 含真实 mihomo / geoip.dat / geosite.dat（只读源）。
#   offline 容器需 --network none；online 容器需可访问回环镜像。
set -eu
BIN=${1:?binary}
SEED=${2:?seed dir}
MODE=${3:?mode offline|online}
PORT=7799
PASS=geo-pass-123
BASE="http://127.0.0.1:$PORT"
WORK=$(mktemp -d)
DATA="$WORK/data"
LOG="$WORK/panel.log"
JAR="$WORK/cookies"
MIRROR_LOG="$WORK/mirror.log"
FAILS=0
PID=""
MPID=""

cleanup() {
  [ -n "$PID" ] && kill -TERM "$PID" 2>/dev/null || true
  [ -n "$MPID" ] && kill -TERM "$MPID" 2>/dev/null || true
  wait 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

ok() { printf '  ok   %s\n' "$1"; }
bad() { printf '  FAIL %s\n' "$1"; FAILS=$((FAILS + 1)); }
contains() { case "$2" in *"$3"*) ok "$1" ;; *) bad "$1（缺少 $3）" ;; esac; }
absent() { case "$2" in *"$3"*) bad "$1（不应含 $3）" ;; *) ok "$1" ;; esac; }

mkdir -p "$DATA/bin" "$DATA/kernel" "$DATA/profiles"
cp "$SEED/mihomo" "$DATA/bin/mihomo"
chmod 0755 "$DATA/bin/mihomo"

ID=abcd1234
cat >"$DATA/profiles/$ID.yaml" <<'YAML'
proxies: []
proxy-groups: []
rules:
  - GEOSITE,cn,DIRECT
  - GEOIP,cn,DIRECT
  - MATCH,DIRECT
dns:
  enable: true
  nameserver-policy:
    "geosite:cn":
      - 223.5.5.5
YAML
NOW=$(date +%s)000
printf '{"activeId":"%s","profiles":[{"id":"%s","name":"geo","type":"local","createdAt":%s,"updatedAt":%s,"settings":{}}]}' \
  "$ID" "$ID" "$NOW" "$NOW" >"$DATA/profiles.json"

MIRROR=""
if [ "$MODE" = online ]; then
  MIRROR="http://127.0.0.1:8899/"
  ROOT="$WORK/mirror/https:/github.com/MetaCubeX/meta-rules-dat/releases/download/latest"
  mkdir -p "$ROOT"
  cp "$SEED/geoip.dat" "$SEED/geosite.dat" "$ROOT/"
  ( cd "$WORK/mirror" && python3 -m http.server 8899 --bind 127.0.0.1 ) >"$MIRROR_LOG" 2>&1 &
  MPID=$!
  i=0
  until curl -sf "http://127.0.0.1:8899/https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat" -o /dev/null 2>&1; do
    i=$((i + 1)); [ "$i" -gt 40 ] && { echo "镜像未就绪"; cat "$MIRROR_LOG"; exit 1; }; sleep 0.25
  done
  ok "本地 geo 镜像就绪"
fi
printf '{"kernel":{"autoStart":true,"mirror":"%s"},"server":{"port":%s,"host":"127.0.0.1"}}' \
  "$MIRROR" "$PORT" >"$DATA/settings.json"

echo "== geo $MODE：启动面板（autoStart）=="
"$BIN" run -p "$PORT" -H 127.0.0.1 -d "$DATA" --password "$PASS" >"$LOG" 2>&1 &
PID=$!
i=0
until curl -sf "$BASE/api/health" >/dev/null 2>&1; do
  i=$((i + 1)); [ "$i" -gt 120 ] && { echo "health 超时"; cat "$LOG"; exit 1; }; sleep 0.5
done
curl -s -c "$JAR" -H 'Content-Type: application/json' -d "{\"password\":\"$PASS\"}" "$BASE/api/auth/login" >/dev/null
STATE=""
i=0
until [ "$STATE" = running ]; do
  i=$((i + 1)); [ "$i" -gt 120 ] && { bad "内核启动超时"; break; }; sleep 0.5
  STATE=$(curl -s -b "$JAR" "$BASE/api/kernel/status" | grep -o '"state":"[a-z]*"' | head -1 | cut -d'"' -f4)
done
[ "$STATE" = running ] && ok "内核 running（geo 不阻塞启动）"
CFG=$(cat "$DATA/kernel/config.yaml" 2>/dev/null || true)
PROFILES=$(curl -s -b "$JAR" "$BASE/api/profiles")

if [ "$MODE" = online ]; then
  echo "== online：镜像预置 + 规则保留 =="
  [ -f "$DATA/kernel/geoip.dat" ] && ok "geoip.dat 预置" || bad "geoip.dat 缺失"
  [ -f "$DATA/kernel/geosite.dat" ] && ok "geosite.dat 预置" || bad "geosite.dat 缺失"
  GS=$(wc -c <"$DATA/kernel/geosite.dat" 2>/dev/null || echo 0)
  [ "$GS" -gt 1000000 ] && ok "geosite.dat 全尺寸（$GS）" || bad "geosite.dat 过小（$GS）"
  contains "保留 GEOSITE 规则" "$CFG" "GEOSITE,cn"
  contains "保留 GEOIP 规则" "$CFG" "GEOIP,cn"
  contains "保留 dns geosite" "$CFG" "geosite:cn"
  contains "geodata-mode:true" "$CFG" "geodata-mode: true"
  contains "geo-auto-update:false" "$CFG" "geo-auto-update: false"
  contains "geox-url 已设" "$CFG" "geox-url:"
  contains "lastConfigError 清空" "$PROFILES" '"lastConfigError":null'
else
  echo "== offline：断网降级 =="
  absent "删除 GEOSITE 规则" "$CFG" "GEOSITE"
  absent "删除 GEOIP 规则" "$CFG" "GEOIP"
  contains "MATCH,DIRECT 回退" "$CFG" "MATCH,DIRECT"
  absent "剥离 dns geosite" "$CFG" "geosite:cn"
  contains "geodata-mode:true" "$CFG" "geodata-mode: true"
  contains "geo-auto-update:false" "$CFG" "geo-auto-update: false"
  [ ! -f "$DATA/kernel/geoip.dat" ] && ok "无 geoip.dat 落盘" || bad "意外下载 geoip.dat"
  [ ! -f "$DATA/kernel/geosite.dat" ] && ok "无 geosite.dat 落盘" || bad "意外下载 geosite.dat"
  case "$PROFILES" in *'"lastConfigError":null'*) bad "lastConfigError 应含警告" ;; *) ok "lastConfigError 保留警告" ;; esac
fi

echo
if [ "$FAILS" -gt 0 ]; then
  echo "geo $MODE 失败：$FAILS 项"; echo "--- panel.log ---"; cat "$LOG" 2>/dev/null || true
  echo "--- config.yaml ---"; printf '%s\n' "$CFG"
  exit 1
fi
echo "geo $MODE 全部通过"