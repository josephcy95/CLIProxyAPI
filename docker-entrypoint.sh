#!/bin/sh
set -e

DATA_DIR="${CLIPROXY_DATA_DIR:-${CLI_PROXY_DATA_DIR:-/data}}"
mkdir -p "$DATA_DIR/auths" "$DATA_DIR/logs" "$DATA_DIR/plugins"

if [ ! -f "$DATA_DIR/config.yaml" ]; then
  if [ -f /CLIProxyAPI/config.example.yaml ]; then
    cp /CLIProxyAPI/config.example.yaml "$DATA_DIR/config.yaml"
    echo "seeded $DATA_DIR/config.yaml from config.example.yaml"
  fi
fi

exec ./CLIProxyAPI "$@"
