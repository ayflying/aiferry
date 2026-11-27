#!/bin/sh
set -eu
umask 077

# 首次启动在专用卷生成密钥；容器重建不轮换、不输出密钥。
if [ ! -e "$AGY_BRIDGE_TOKEN_FILE" ]; then
    tmp=$(mktemp "${AGY_BRIDGE_TOKEN_FILE}.XXXXXX")
    trap 'rm -f "$tmp"' EXIT
    openssl rand -hex 32 > "$tmp"
    ln "$tmp" "$AGY_BRIDGE_TOKEN_FILE" || [ -s "$AGY_BRIDGE_TOKEN_FILE" ]
    rm -f "$tmp"
    trap - EXIT
fi
exec agy-bridge
