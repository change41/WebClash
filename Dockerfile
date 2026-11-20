# WebClash 运行时镜像：构建前端 → 交叉编译 Go 静态二进制 → alpine 运行时。
# 构建阶段固定在 BUILDPLATFORM 原生交叉编译（CGO_ENABLED=0）；多架构下仅末尾 alpine 层需要 QEMU。
# 受限网络可用 --build-arg APT_MIRROR/APK_MIRROR/GOPROXY 替换包源。

FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS web
WORKDIR /app
COPY package.json package-lock.json ./
COPY web/package.json web/
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ web/
RUN npm run build -w web

FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS go
ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.google.cn
ENV GOPROXY=${GOPROXY} GOSUMDB=${GOSUMDB}
WORKDIR /src
COPY panel/go.mod panel/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY panel/ ./
COPY --from=web /app/web/dist/ ./internal/webui/dist/
ARG VERSION=0.1.0
ARG BUILD_TIME=
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    BT="${BUILD_TIME:-$(date +%s%3N)}"; \
    CGO_ENABLED=0 GOOS=linux GOARCH="${TARGETARCH:-amd64}" go build -trimpath \
      -ldflags="-s -w -X webclash/internal/buildinfo.Version=${VERSION} -X webclash/internal/buildinfo.BuildTime=${BT}" \
      -o /out/webclash ./cmd/webclash

FROM alpine:3.20
ARG APK_MIRROR=
RUN set -eux; \
    if [ -n "$APK_MIRROR" ]; then sed -i "s|dl-cdn.alpinelinux.org|$APK_MIRROR|g" /etc/apk/repositories; fi; \
    apk add --no-cache ca-certificates tzdata
ENV DATA_DIR=/data \
    WEBUI_HOST=0.0.0.0 \
    WEBUI_PORT=7788 \
    TZ=Asia/Shanghai
WORKDIR /app
COPY --from=go /out/webclash /usr/local/bin/webclash
RUN mkdir -p /data
VOLUME ["/data"]
EXPOSE 7788
ENTRYPOINT ["/usr/local/bin/webclash"]
CMD ["run"]