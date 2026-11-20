FROM alpine:3.20
ARG APK_MIRROR=
RUN set -eux; \
    if [ -n "$APK_MIRROR" ]; then sed -i "s|dl-cdn.alpinelinux.org|$APK_MIRROR|g" /etc/apk/repositories; fi; \
    apk add --no-cache curl python3 procps