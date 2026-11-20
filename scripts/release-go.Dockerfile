# Go release pipeline: build the React frontend, embed it, cross-compile static
# binaries (CGO_ENABLED=0) for linux + darwin (amd64/arm64), package tar.xz
# (linux) / tar.gz (darwin) + sums. Build runs natively (no QEMU for cross-compile).

FROM node:24-bookworm-slim AS web
WORKDIR /app
COPY package.json package-lock.json ./
COPY web/package.json web/
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ web/
RUN npm run build -w web

FROM golang:1.27-bookworm AS go
ENV GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=sum.golang.google.cn
# APT_MIRROR 可选：受限网络可替换 Debian 源（如 mirrors.aliyun.com），默认官方源。
ARG APT_MIRROR=
RUN set -eux; \
    if [ -n "$APT_MIRROR" ]; then \
      sed -i "s|deb.debian.org|$APT_MIRROR|g; s|security.debian.org|$APT_MIRROR|g" \
        /etc/apt/sources.list.d/debian.sources; \
    fi; \
    apt-get update \
    && apt-get install -y --no-install-recommends xz-utils \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY panel/go.mod panel/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY panel/ ./
COPY --from=web /app/web/dist/ ./internal/webui/dist/
ARG VERSION=0.1.0
ARG BUILD_TIME=
ARG TARGETS=linux/amd64,linux/arm64,darwin/amd64,darwin/arm64
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    BT="${BUILD_TIME:-$(date +%s%3N)}"; \
    for T in $(echo "$TARGETS" | tr ',' ' '); do \
      OS="${T%/*}"; A="${T#*/}"; \
      case "$A" in amd64) N=x64;; arm64) N=arm64;; *) N="$A";; esac; \
      CGO_ENABLED=0 GOOS="$OS" GOARCH="$A" go build -trimpath \
        -ldflags="-s -w -X webclash/internal/buildinfo.Version=${VERSION} -X webclash/internal/buildinfo.BuildTime=${BT}" \
        -o /out/webclash-"$OS"-"$N" ./cmd/webclash; \
    done

FROM go AS package
RUN set -eux; cd /out; \
    for f in webclash-linux-x64 webclash-linux-arm64; do \
      if [ -f "$f" ]; then tar -cJf "$f.tar.xz" "$f"; fi; \
    done; \
    for f in webclash-darwin-x64 webclash-darwin-arm64; do \
      if [ -f "$f" ]; then tar -czf "$f.tar.gz" "$f"; fi; \
    done; \
    sha256sum webclash-* > SHA256SUMS
COPY scripts/install.sh /out/install.sh

FROM scratch AS export
COPY --from=package /out/ /