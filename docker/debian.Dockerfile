# syntax=docker/dockerfile:1
FROM debian:trixie-slim

ARG TARGETPLATFORM

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system regimux \
    && useradd --system --gid regimux --home-dir /var/lib/regimux --shell /usr/sbin/nologin regimux \
    && mkdir -p /etc/regimux /var/lib/regimux \
    && chown -R regimux:regimux /etc/regimux /var/lib/regimux

COPY --chmod=0755 ${TARGETPLATFORM}/regimuxd /usr/local/bin/regimuxd
COPY configs/regimux.minimal.hcl /etc/regimux/regimux.hcl

WORKDIR /var/lib/regimux
USER regimux
EXPOSE 8080
VOLUME ["/var/lib/regimux"]

ENTRYPOINT ["/usr/local/bin/regimuxd"]
CMD ["--config", "/etc/regimux/regimux.hcl"]
