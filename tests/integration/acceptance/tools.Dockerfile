FROM node:22-alpine AS node
FROM python:3.12-alpine AS bootstrap
COPY --from=node /usr/local/bin/node /usr/local/bin/node
RUN apk add --no-cache libstdc++ \
    && python -m pip install --no-cache-dir --disable-pip-version-check uv==0.11.23
ENV UV_PYTHON_DOWNLOADS=never
WORKDIR /opt/acceptance

FROM bootstrap AS tools
COPY pyproject.toml uv.lock ./
RUN uv sync --frozen \
    && adduser -D -u 10001 acceptance \
    && chown -R acceptance:acceptance /opt/acceptance
USER acceptance
ENTRYPOINT ["uv", "run", "--frozen", "--project", "/opt/acceptance"]
