#!/bin/sh
set -eu
umask 077

# 首次启动在专用卷生成默认账号的密钥；容器重建不轮换、不输出密钥。
# 多账号部署在 accounts/<名字>/token 各放一把令牌（至少 32 字节），每个账号
# 各自在 accounts/<名字>/home 里完成一次官方 agy 登录；默认账号与之并存。
if [ ! -e "$AGY_BRIDGE_TOKEN_FILE" ]; then
    tmp=$(mktemp "${AGY_BRIDGE_TOKEN_FILE}.XXXXXX")
    trap 'rm -f "$tmp"' EXIT
    openssl rand -hex 32 > "$tmp"
    ln "$tmp" "$AGY_BRIDGE_TOKEN_FILE" || [ -s "$AGY_BRIDGE_TOKEN_FILE" ]
    rm -f "$tmp"
    trap - EXIT
fi
exec agy-bridge
