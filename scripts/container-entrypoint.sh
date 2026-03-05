#!/usr/bin/env sh
set -eu

if [ "$#" -eq 0 ]; then
  set -- serve --port 3000
fi

exec /usr/local/bin/atlaskb "$@"
