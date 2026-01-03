#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a

  # Avoid inheriting an exported API_KEY from the parent shell when .env doesn't define one.
  if ! rg -q "^\s*API_KEY=" ./.env; then
    unset API_KEY
  fi
fi

BIN_DIR="$ROOT_DIR/bin"
BIN="$BIN_DIR/refactor-server"

needs_rebuild() {
  [[ ! -x "$BIN" ]] && return 0

  local bin_mtime latest_src
  bin_mtime="$(stat -c %Y "$BIN")"

  latest_src="$(
    find "$ROOT_DIR" \
      -type f \
      \( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' \) \
      -print0 \
    | xargs -0 stat -c %Y \
    | sort -n \
    | tail -n 1
  )"

  [[ -z "${latest_src:-}" ]] && return 0
  (( latest_src > bin_mtime )) && return 0
  return 1
}

mkdir -p "$BIN_DIR"

if needs_rebuild; then
  echo "[start] code changed -> building..."
  go build -o "$BIN" ./cmd/server
else
  echo "[start] no code changes -> using existing binary"
fi

echo "[start] running: $BIN"
exec "$BIN" "$@"
