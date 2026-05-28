# syntax=docker/dockerfile:1
FROM alpine:3.22

ARG TARGETPLATFORM

RUN apk add --no-cache ca-certificates \
    && addgroup -S regimux \
    && adduser -S -G regimux -h /var/lib/regimux regimux \
    && mkdir -p /etc/regimux /var/lib/regimux \
    && chown -R regimux:regimux /etc/regimux /var/lib/regimux

COPY --chmod=0755 ${TARGETPLATFORM}/regimuxd /usr/local/bin/regimuxd
COPY configs/regimux.minimal.hcl /etc/regimux/regimux.hcl

WORKDIR /var/lib/regimux
USER regimux
EXPOSE 5000
VOLUME ["/var/lib/regimux"]

ENTRYPOINT ["/usr/local/bin/regimuxd"]
CMD ["--config", "/etc/regimux/regimux.hcl"]
