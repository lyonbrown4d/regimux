FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 regimux \
    && mkdir -p /var/lib/regimux \
    && chown regimux:regimux /var/lib/regimux
COPY --chmod=0755 regimuxd /usr/local/bin/regimuxd
USER regimux
WORKDIR /var/lib/regimux
ENTRYPOINT ["/usr/local/bin/regimuxd"]
CMD ["--config", "/etc/regimux/acceptance.hcl"]
