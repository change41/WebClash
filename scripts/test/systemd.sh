#!/bin/sh
# 在 systemd 容器内执行：安装/启停/升级/卸载/清数据 全生命周期。
# 用法: sh systemd.sh <binary>
set -eu
SRC=${1:?binary path}
FAILS=0
ok() { printf '  ok   %s\n' "$1"; }
bad() { printf '  FAIL %s\n' "$1"; FAILS=$((FAILS + 1)); }
check() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1（期望 $3，实际 $2）"; fi; }

cp "$SRC" /root/webclash
chmod 0755 /root/webclash
BIN=/usr/local/bin/webclash

echo "== 等待 systemd 就绪 =="
i=0
until systemctl is-system-running >/dev/null 2>&1 || [ "$(systemctl is-system-running 2>/dev/null)" = "degraded" ]; do
  i=$((i + 1)); [ "$i" -gt 60 ] && { echo "systemd 未就绪"; exit 1; }
  sleep 1
done
ok "systemd 就绪"

echo "== dry-run 零落盘 =="
/root/webclash install --dry-run >/root/dry.txt 2>&1
grep -q "would-write" /root/dry.txt && ok "dry-run 输出动作" || bad "dry-run 无动作"
[ ! -e /etc/systemd/system/webclash.service ] && ok "dry-run 未写 unit" || bad "dry-run 写了 unit"
[ ! -e /etc/webclash/webclash.env ] && ok "dry-run 未写 env" || bad "dry-run 写了 env"

echo "== install =="
/root/webclash install --port 7788 >/root/install.txt 2>&1 || { cat /root/install.txt; exit 1; }
check "is-active" "$(systemctl is-active webclash)" "active"
i=0; until curl -sf http://127.0.0.1:7788/api/health >/dev/null 2>&1; do i=$((i+1)); [ "$i" -gt 30 ] && break; sleep 1; done
curl -sf http://127.0.0.1:7788/api/health >/dev/null && ok "health" || bad "health"
/root/webclash status --json | grep -q '"healthy": true' && ok "status --json healthy" || bad "status json"
/root/webclash password -d /var/lib/webclash >/root/pw.txt && [ -s /root/pw.txt ] && ok "password 可读" || bad "password"

echo "== unit 关键配置 =="
SHOW=$(systemctl show webclash -p KillMode -p Restart -p KillSignal)
contains() { case "$2" in *"$3"*) ok "$1" ;; *) bad "$1（缺 $3）" ;; esac; }
contains "KillMode=mixed" "$SHOW" "KillMode=mixed"
contains "Restart=on-failure" "$SHOW" "Restart=on-failure"
contains "KillSignal=SIGTERM" "$SHOW" "KillSignal=15"

echo "== stop / start / restart =="
/root/webclash stop >/dev/null 2>&1
check "is-active after stop" "$(systemctl is-active webclash)" "inactive"
rc=0; /root/webclash status >/dev/null 2>&1 || rc=$?
check "status exit after stop" "$rc" "3"
/root/webclash start >/dev/null 2>&1
check "is-active after start" "$(systemctl is-active webclash)" "active"
/root/webclash restart >/dev/null 2>&1
check "is-active after restart" "$(systemctl is-active webclash)" "active"

echo "== 升级 + 数据保留 =="
sha256sum /var/lib/webclash/settings.json >/root/before.sha
/root/webclash install --force >/dev/null 2>&1
sha256sum -c /root/before.sha >/dev/null 2>&1 && ok "升级后数据不变" || bad "升级后数据变化"
check "升级后 active" "$(systemctl is-active webclash)" "active"

echo "== uninstall 保留数据 =="
/root/webclash uninstall >/dev/null 2>&1
[ ! -e /etc/systemd/system/webclash.service ] && ok "unit 已删" || bad "unit 仍在"
[ -d /var/lib/webclash ] && ok "数据保留" || bad "数据被删"
[ -e /etc/webclash/webclash.env ] && ok "env 保留" || bad "env 被删"

echo "== reinstall 复用数据 =="
/root/webclash install >/dev/null 2>&1
check "reinstall active" "$(systemctl is-active webclash)" "active"
sha256sum -c /root/before.sha >/dev/null 2>&1 && ok "复用原数据" || bad "数据未复用"

echo "== purge =="
/root/webclash uninstall --purge --yes >/dev/null 2>&1
[ ! -d /var/lib/webclash ] && ok "数据已清" || bad "数据未清"
[ ! -e "$BIN" ] && ok "二进制已删" || bad "二进制仍在"

echo
[ "$FAILS" -gt 0 ] && { echo "systemd 验证失败：$FAILS 项"; exit 1; }
echo "systemd 全生命周期通过"