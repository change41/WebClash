FROM debian:bookworm-slim

ENV container=docker
ARG APT_MIRROR=
RUN set -eux; \
    if [ -n "$APT_MIRROR" ]; then \
      sed -i "s|deb.debian.org|$APT_MIRROR|g; s|security.debian.org|$APT_MIRROR|g" \
        /etc/apt/sources.list.d/debian.sources; \
    fi; \
    apt-get update \
    && apt-get install -y --no-install-recommends systemd systemd-sysv dbus curl ca-certificates procps \
    && rm -rf /var/lib/apt/lists/*

RUN systemctl mask systemd-udevd.service systemd-udevd-kernel.socket systemd-udevd-control.socket \
      systemd-modules-load.service sys-kernel-debug.mount sys-kernel-config.mount getty.target \
  && rm -f /etc/machine-id && touch /etc/machine-id

STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]