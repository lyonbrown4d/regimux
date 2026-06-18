#!/bin/sh
set -eu

concurrency="${REGIMUX_INTEGRATION_CONCURRENCY:-4}"
status=0
export PIP_DISABLE_PIP_VERSION_CHECK=1

for i in $(seq 1 "$concurrency"); do
  (
    python -m pip download six==1.17.0 \
      --no-deps \
      --only-binary=:all: \
      --index-url http://regimux:8080/pypi/default/simple \
      --trusted-host regimux \
      --timeout 60 \
      --retries 5 \
      --cache-dir "/tmp/pip-cache-$i" \
      -d "/tmp/pip-$i"
    test -f "/tmp/pip-$i/six-1.17.0-py2.py3-none-any.whl"
  ) &
done

for pid in $(jobs -p); do
  wait "$pid" || status=1
done

exit "$status"
