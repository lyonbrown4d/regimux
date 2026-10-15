#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
uv_bin=$(command -v uv || true)
if [ -z "$uv_bin" ]; then
  printf '%s\n' "PyPI acceptance requires uv in PATH" >&2
  exit 1
fi

exec "$uv_bin" run --no-project python "$script_dir/pypi-client.py"
