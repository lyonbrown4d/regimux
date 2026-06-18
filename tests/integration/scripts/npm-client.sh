#!/bin/sh
set -eu

concurrency="${REGIMUX_INTEGRATION_CONCURRENCY:-4}"
status=0

for i in $(seq 1 "$concurrency"); do
  (
    work="/tmp/npm-$i"
    mkdir -p "$work"
    cd "$work"
    npm --cache "/tmp/npm-cache-$i" --registry=http://regimux:8080/npm/default --ignore-scripts --loglevel=error pack lodash@4.17.21 >/dev/null
    test -f lodash-4.17.21.tgz
  ) &
done

for pid in $(jobs -p); do
  wait "$pid" || status=1
done

exit "$status"
