#!/usr/bin/env bash
# 发行版验证编排（宿主机只调用 docker，不直接执行新二进制）。
# Go 静态二进制：glibc/musl 通用，无需 Node/libstdc++；产物在 dist-go/。
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
DIST="$ROOT/dist-go"
TEST="$ROOT/scripts/test"

[ -f "$DIST/SHA256SUMS" ] || { echo "缺少 dist-go/SHA256SUMS，请先运行 npm run release:go"; exit 1; }

case "$(uname -m)" in
  arm64|aarch64) NATIVE=arm64; OTHER=x64; OTHER_PLATFORM=linux/amd64 ;;
  *) NATIVE=x64; OTHER=arm64; OTHER_PLATFORM=linux/arm64 ;;
esac
NATIVE_PLATFORM="linux/$NATIVE"
MODE=${1:-all}

section() { printf '\n========== %s ==========\n' "$1"; }

# APT_MIRROR 可选：受限网络替换 Debian/Ubuntu 源（如 mirrors.aliyun.com）。
APT_MIRROR=${APT_MIRROR:-}
# APK_MIRROR 可选：受限网络替换 Alpine 源；缺省复用 APT_MIRROR（同一家镜像站）。
APK_MIRROR=${APK_MIRROR:-$APT_MIRROR}
apt_mirror_cmd() {
  [ -n "$APT_MIRROR" ] || return 0
  printf 'for f in /etc/apt/sources.list /etc/apt/sources.list.d/debian.sources; do [ -f "$f" ] && sed -i "s|deb.debian.org|%s|g; s|security.debian.org|%s|g; s|archive.ubuntu.com|%s|g; s|security.ubuntu.com|%s|g" "$f"; done; ' \
    "$APT_MIRROR" "$APT_MIRROR" "$APT_MIRROR" "$APT_MIRROR"
}

smoke_debian() { # platform binary image
  docker run --rm --platform "$1" -v "$DIST:/pkg:ro" -v "$TEST:/test:ro" "$3" \
    sh -c "$(apt_mirror_cmd)apt-get update >/dev/null && apt-get install -y --no-install-recommends curl ca-certificates >/dev/null && sh /test/smoke.sh /pkg/$2"
}

smoke_alpine() { # platform binary（静态二进制，无需 libstdc++）
  docker run --rm --platform "$1" -v "$DIST:/pkg:ro" -v "$TEST:/test:ro" alpine:3.20 \
    sh -c "apk add --no-cache curl ca-certificates >/dev/null && sh /test/smoke.sh /pkg/$2"
}

if [ "$MODE" = all ] || [ "$MODE" = smoke ]; then
  section "V0 体积断言（< 40MB）"
  for f in webclash-linux-x64 webclash-linux-arm64; do
    [ -f "$DIST/$f" ] || continue
    SZ=$(wc -c <"$DIST/$f")
    if [ "$SZ" -lt 41943040 ]; then echo "  ok   $f $SZ bytes (<40MB)"; else echo "  FAIL $f $SZ bytes 超 40MB"; exit 1; fi
  done

  section "V1 glibc 冒烟（$NATIVE, bookworm）"
  smoke_debian "$NATIVE_PLATFORM" "webclash-linux-$NATIVE" debian:bookworm-slim
  section "V2 glibc 边界（$NATIVE, ubuntu 20.04 / glibc 2.31）"
  smoke_debian "$NATIVE_PLATFORM" "webclash-linux-$NATIVE" ubuntu:20.04
  section "V3 musl 冒烟（$NATIVE, alpine，无 libstdc++）"
  smoke_alpine "$NATIVE_PLATFORM" "webclash-linux-$NATIVE"
  section "V4 glibc 冒烟（$OTHER, bookworm）"
  smoke_debian "$OTHER_PLATFORM" "webclash-linux-$OTHER" debian:bookworm-slim
  section "V5 musl 冒烟（$OTHER, alpine，无 libstdc++）"
  smoke_alpine "$OTHER_PLATFORM" "webclash-linux-$OTHER"

  section "V8 dry-run 零落盘"
  docker run --rm -v "$DIST:/pkg:ro" debian:bookworm-slim sh -c "
    /pkg/webclash-linux-$NATIVE install --dry-run --service-manager systemd >/dev/null
    /pkg/webclash-linux-$NATIVE install --dry-run --service-manager openrc >/dev/null
    test ! -e /etc/systemd/system/webclash.service
    test ! -e /etc/init.d/webclash
    test ! -e /etc/webclash/webclash.env
    test ! -e /usr/local/bin/webclash.new
    echo V8-ok"

  section "V12 install.sh 离线全流程（${NATIVE}）"
  docker run --rm -v "$DIST:/pkg:ro" -v "$ROOT/scripts:/scripts:ro" debian:bookworm-slim sh -c "
    $(apt_mirror_cmd)apt-get update >/dev/null && apt-get install -y --no-install-recommends curl ca-certificates xz-utils >/dev/null
    sh /scripts/install.sh --local /pkg/webclash-linux-$NATIVE.tar.xz --no-service
    /usr/local/bin/webclash version"
fi

if [ "$MODE" = all ] || [ "$MODE" = service ]; then
  section "V6/V9 systemd 全生命周期"
  docker build -q --build-arg APT_MIRROR="$APT_MIRROR" -f "$TEST/systemd.Dockerfile" -t wc-systemd-test "$ROOT" >/dev/null
  docker rm -f wc-systemd-run >/dev/null 2>&1 || true
  docker run -d --name wc-systemd-run --privileged --cgroupns=host \
    --tmpfs /run --tmpfs /run/lock -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
    -v "$DIST:/pkg:ro" -v "$TEST:/test:ro" wc-systemd-test >/dev/null
  trap 'docker rm -f wc-systemd-run >/dev/null 2>&1 || true' EXIT
  docker exec wc-systemd-run sh /test/systemd.sh "/pkg/webclash-linux-$NATIVE"
  docker rm -f wc-systemd-run >/dev/null 2>&1 || true
  trap - EXIT

  section "V7 OpenRC 全生命周期"
  docker build -q --build-arg APK_MIRROR="$APK_MIRROR" -f "$TEST/openrc.Dockerfile" -t wc-openrc-test "$ROOT" >/dev/null
  docker rm -f wc-openrc-run >/dev/null 2>&1 || true
  docker run -d --name wc-openrc-run --privileged --cgroupns=host \
    -v /sys/fs/cgroup:/sys/fs/cgroup:ro \
    -v "$DIST:/pkg:ro" -v "$TEST:/test:ro" wc-openrc-test >/dev/null
  trap 'docker rm -f wc-openrc-run >/dev/null 2>&1 || true' EXIT
  docker exec wc-openrc-run sh /test/openrc.sh "/pkg/webclash-linux-$NATIVE"
  docker rm -f wc-openrc-run >/dev/null 2>&1 || true
  trap - EXIT
fi

if [ "$MODE" = all ] || [ "$MODE" = geo ]; then
  section "V13 Geo（offline 降级 + online 预置 + 空闲 RSS）"
  # 需要真实 mihomo + geoip.dat/geosite.dat 种子。从既有测试卷 wc-go-g7-on-data
  # 制备 wc-go-geo-seed；不存在则跳过（geo 行为已在 G7 用真实数据验证）。
  if docker volume inspect wc-go-g7-on-data >/dev/null 2>&1; then
    docker volume create wc-go-geo-seed >/dev/null
    docker run --rm -v wc-go-g7-on-data:/from:ro -v wc-go-geo-seed:/to alpine:3.20 sh -c '
      mkdir -p /to && cp /from/bin/mihomo /to/mihomo && cp /from/kernel/geoip.dat /from/kernel/geosite.dat /to/'
    docker build -q --build-arg APK_MIRROR="$APK_MIRROR" -f "$TEST/geo.Dockerfile" -t wc-geo-test "$ROOT" >/dev/null
    echo "-- online（回环镜像预置真实 geo）--"
    docker run --rm -v "$DIST:/pkg:ro" -v "$TEST:/test:ro" -v wc-go-geo-seed:/seed:ro wc-geo-test \
      sh /test/geo.sh "/pkg/webclash-linux-$NATIVE" /seed online
    echo "-- offline（--network none 断网降级）--"
    docker run --rm --network none -v "$DIST:/pkg:ro" -v "$TEST:/test:ro" -v wc-go-geo-seed:/seed:ro wc-geo-test \
      sh /test/geo.sh "/pkg/webclash-linux-$NATIVE" /seed offline
    echo "-- 空闲 RSS（预置 geo，无下载突发，稳态 < 40MB）--"
    docker run --rm -v "$DIST:/pkg:ro" -v wc-go-geo-seed:/seed:ro wc-geo-test sh -c '
      W=$(mktemp -d); D=$W/data; mkdir -p "$D/bin" "$D/kernel" "$D/profiles"
      cp /seed/mihomo "$D/bin/mihomo"; chmod 0755 "$D/bin/mihomo"
      cp /seed/geoip.dat /seed/geosite.dat "$D/kernel/"
      printf "proxies: []\nrules:\n  - GEOSITE,cn,DIRECT\n  - GEOIP,cn,DIRECT\n  - MATCH,DIRECT\n" > "$D/profiles/abcd1234.yaml"
      printf "{\"activeId\":\"abcd1234\",\"profiles\":[{\"id\":\"abcd1234\",\"name\":\"g\",\"type\":\"local\",\"createdAt\":1,\"updatedAt\":1,\"settings\":{}}]}" > "$D/profiles.json"
      printf "{\"kernel\":{\"autoStart\":true},\"server\":{\"port\":7799,\"host\":\"127.0.0.1\"}}" > "$D/settings.json"
      /pkg/webclash-linux-'"$NATIVE"' run -p 7799 -H 127.0.0.1 -d "$D" --password rss-pass-123 > "$W/log" 2>&1 &
      P=$!
      i=0; until curl -sf http://127.0.0.1:7799/api/health >/dev/null 2>&1; do i=$((i+1)); [ "$i" -gt 60 ] && { cat "$W/log"; exit 1; }; sleep 0.5; done
      sleep 3
      R=$(grep VmRSS "/proc/$P/status" | awk "{print \$2}")
      kill -TERM "$P"; wait 2>/dev/null; rm -rf "$W"
      echo "  面板稳态 VmRSS=${R}kB"
      [ "$R" -lt 40960 ] && echo "  ok   RSS < 40MB" || { echo "  FAIL RSS 超 40MB"; exit 1; }'
  else
    echo "  skip 缺少 wc-go-g7-on-data 种子卷（mihomo+geo），跳过 V13"
  fi
fi

if [ "$MODE" = all ] || [ "$MODE" = darwin ]; then
  section "V14 darwin 编译/格式/vet（无运行时冒烟）"
  # 宿主机 file/wc 只读检查，绝不执行 darwin 二进制。
  for f in webclash-darwin-x64 webclash-darwin-arm64; do
    [ -f "$DIST/$f" ] || { echo "  FAIL 缺少 $f（请先 npm run release:go）"; exit 1; }
    SZ=$(wc -c <"$DIST/$f")
    [ "$SZ" -lt 41943040 ] || { echo "  FAIL $f $SZ bytes 超 40MB"; exit 1; }
    FT=$(file -b "$DIST/$f")
    case "$FT" in
      *"Mach-O 64-bit"*) echo "  ok   $f $SZ bytes | $FT" ;;
      *) echo "  FAIL $f 非 Mach-O 64-bit: $FT"; exit 1 ;;
    esac
  done
  # GOOS=darwin go vet（arm64 + amd64）：容器内交叉静态检查，不执行 darwin 二进制。
  docker run --rm -v "$ROOT/panel:/src" -w /src \
    -v wc-go-vet-mod:/go/pkg/mod -v wc-go-vet-cache:/root/.cache/go-build \
    -e GOPROXY=https://goproxy.cn,direct -e GOSUMDB=sum.golang.google.cn \
    -e GOOS=darwin -e CGO_ENABLED=0 \
    golang:1.27-bookworm sh -c '
      set -eu
      GOARCH=arm64 go vet ./... && echo "  ok   GOOS=darwin GOARCH=arm64 go vet"
      GOARCH=amd64 go vet ./... && echo "  ok   GOOS=darwin GOARCH=amd64 go vet"'
fi

printf '\n全部验证通过\n'