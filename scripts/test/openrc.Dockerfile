FROM alpine:3.20

ARG APK_MIRROR=
RUN set -eux; \
    if [ -n "$APK_MIRROR" ]; then sed -i "s|dl-cdn.alpinelinux.org|$APK_MIRROR|g" /etc/apk/repositories; fi; \
    apk add --no-cache openrc curl ca-certificates

RUN sed -i 's/^#rc_sys=""/rc_sys="lxc"/' /etc/rc.conf \
  && rm -f /etc/init.d/agetty /etc/init.d/getty \
  && mkdir -p /run/openrc \
  && touch /run/openrc/softlevel

CMD ["/sbin/init"]