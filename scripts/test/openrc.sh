#!/bin/sh
# 在 OpenRC 容器内执行：安装/启停/卸载 全生命周期。
# 用法: sh openrc.sh <binary>
set -eu
SRC=${1:?binary path}
FAILS=0
ok() { printf '  ok   %s\n' "$1"; }
bad() { printf '  FAIL %s\n' "$1"; FAILS=$((FAILS + 1)); }
check() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1（期望 $3，实际 $2）"; fi; }

cp "$SRC" /root/webclash
chmod 0755 /root/webclash

echo "== 引导 OpenRC =="
if ! rc-service webclash status >/dev/null 2>&1 && [ ! -e /run/openrc/softlevel ]; then
  openrc sysinit >/dev/null 2>&1 || true
  openrc boot >/dev/null 2>&1 || true
  openrc default >/dev/null 2>&1 || true
fi
ok "OpenRC 引导"

echo "== init 语法 =="
/root/webclash install --dry-run --service-manager openrc >/root/dry.txt 2>&1
grep -q "would-write" /root/dry.txt && ok "dry-run 输出动作" || bad "dry-run 无动作"
[ ! -e /etc/init.d/webclash ] && ok "dry-run 未写 init" || bad "dry-run 写了 init"

echo "== install =="
/root/webclash install --service-manager openrc --port 7788 >/root/install.txt 2>&1 || { cat /root/install.txt; exit 1; }
sh -n /etc/init.d/webclash && ok "init 语法合法" || bad "init 语法错误"
rc-update show default | grep -q webclash && ok "已加入 default 运行级" || bad "未加入运行级"
i=0; until curl -sf http://127.0.0.1:7788/api/health >/dev/null 2>&1; do i=$((i+1)); [ "$i" -gt 30 ] && break; sleep 1; done
curl -sf http://127.0.0.1:7788/api/health >/dev/null && ok "health" || bad "health"
rc-service webclash status >/dev/null 2>&1 && ok "rc-service status" || bad "rc-service status"

echo "== stop / start / restart =="
/root/webclash stop --service-manager openrc >/dev/null 2>&1
rc=0; /root/webclash status --service-manager openrc >/dev/null 2>&1 || rc=$?
check "status exit after stop" "$rc" "3"
/root/webclash start --service-manager openrc >/dev/null 2>&1
i=0; until curl -sf http://127.0.0.1:7788/api/health >/dev/null 2>&1; do i=$((i+1)); [ "$i" -gt 30 ] && break; sleep 1; done
curl -sf http://127.0.0.1:7788/api/health >/dev/null && ok "start 后 health" || bad "start 后 health"
/root/webclash restart --service-manager openrc >/dev/null 2>&1
ok "restart"

echo "== uninstall 保留数据 =="
/root/webclash uninstall --service-manager openrc >/dev/null 2>&1
[ ! -e /etc/init.d/webclash ] && ok "init 已删" || bad "init 仍在"
[ -d /var/lib/webclash ] && ok "数据保留" || bad "数据被删"

echo "== purge =="
/root/webclash install --service-manager openrc >/dev/null 2>&1
/root/webclash uninstall --service-manager openrc --purge --yes >/dev/null 2>&1
[ ! -d /var/lib/webclash ] && ok "数据已清" || bad "数据未清"

echo
[ "$FAILS" -gt 0 ] && { echo "OpenRC 验证失败：$FAILS 项"; exit 1; }
echo "OpenRC 全生命周期通过"