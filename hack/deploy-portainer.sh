#!/bin/sh
# 通过 Portainer API 触发线上 stack 重新部署，强制拉取最新镜像。
#
# 必需环境变量：
#   PORTAINER_URL   Portainer 访问地址
#   PORTAINER_KEY   Portainer API 访问令牌（X-API-Key）
# 可选环境变量：
#   ENDPOINT_NAME   目标 endpoint 名称，默认 yunloli
#   STACK_NAME      目标 stack 名称，默认 aiferry
#   DRY_RUN         设为 1 时仅探测，不触发部署
set -eu

ENDPOINT_NAME="${ENDPOINT_NAME:-yunloli}"
STACK_NAME="${STACK_NAME:-aiferry}"

if [ -z "${PORTAINER_URL:-}" ] || [ -z "${PORTAINER_KEY:-}" ]; then
  echo "错误：未设置 PORTAINER_URL / PORTAINER_KEY，无法执行部署" >&2
  exit 1
fi

PORTAINER_URL="${PORTAINER_URL%/}"

api() {
  curl -fsS --max-time 600 -H "X-API-Key: ${PORTAINER_KEY}" "$@"
}

echo "==> 查询 endpoint：${ENDPOINT_NAME}"
ENDPOINTS=$(api "${PORTAINER_URL}/api/endpoints")
ENDPOINT_ID=$(printf '%s' "$ENDPOINTS" | jq -r --arg n "$ENDPOINT_NAME" '.[] | select(.Name == $n) | .Id' | head -n1)
if [ -z "$ENDPOINT_ID" ] || [ "$ENDPOINT_ID" = "null" ]; then
  echo "错误：未找到 endpoint ${ENDPOINT_NAME}，当前可用 endpoint：" >&2
  printf '%s' "$ENDPOINTS" | jq -r '.[] | "  - \(.Name) (Id=\(.Id))"' >&2
  exit 1
fi
echo "   endpointId=${ENDPOINT_ID}"

echo "==> 查询 stack：${STACK_NAME}"
STACKS=$(api "${PORTAINER_URL}/api/stacks")
STACK_ID=$(printf '%s' "$STACKS" | jq -r --arg n "$STACK_NAME" --argjson e "$ENDPOINT_ID" '.[] | select(.Name == $n and .EndpointId == $e) | .Id' | head -n1)
if [ -z "$STACK_ID" ] || [ "$STACK_ID" = "null" ]; then
  echo "错误：未找到 stack ${STACK_NAME}，该 endpoint 上的 stack：" >&2
  printf '%s' "$STACKS" | jq -r --argjson e "$ENDPOINT_ID" '.[] | select(.EndpointId == $e) | "  - \(.Name) (Id=\(.Id), Type=\(.Type))"' >&2
  exit 1
fi
echo "   stackId=${STACK_ID}"

STACK_DETAIL=$(api "${PORTAINER_URL}/api/stacks/${STACK_ID}")
IS_GIT=$(printf '%s' "$STACK_DETAIL" | jq -r 'if .GitConfig then "yes" else "no" end')

if [ "${DRY_RUN:-0}" = "1" ]; then
  echo "==> [DRY_RUN] 探测完成，跳过实际部署（类型：${IS_GIT}）"
  exit 0
fi

if [ "$IS_GIT" = "yes" ]; then
  echo "==> 触发 git stack redeploy（pullImage=true）"
  api -X PUT -H "Content-Type: application/json" \
    "${PORTAINER_URL}/api/stacks/${STACK_ID}/git/redeploy?endpointId=${ENDPOINT_ID}" \
    -d '{"pullImage":true,"prune":false}' >/dev/null
else
  echo "==> 触发 compose stack redeploy（pullImage=true）"
  STACK_FILE=$(printf '%s' "$STACK_DETAIL" | jq -r '.StackFileContent')
  if [ -z "$STACK_FILE" ] || [ "$STACK_FILE" = "null" ]; then
    STACK_FILE=$(api "${PORTAINER_URL}/api/stacks/${STACK_ID}/file" | jq -r '.StackFileContent')
  fi
  if [ -z "$STACK_FILE" ] || [ "$STACK_FILE" = "null" ]; then
    echo "错误：无法读取 stack compose 内容" >&2
    exit 1
  fi
  ENV_JSON=$(printf '%s' "$STACK_DETAIL" | jq -c '.Env // []')
  BODY=$(jq -n --arg f "$STACK_FILE" --argjson e "$ENV_JSON" \
    '{stackFileContent:$f,env:$e,prune:false,pullImage:true}')
  api -X PUT -H "Content-Type: application/json" \
    "${PORTAINER_URL}/api/stacks/${STACK_ID}?endpointId=${ENDPOINT_ID}" \
    -d "$BODY" >/dev/null
fi

echo "==> Portainer 已触发部署：${STACK_NAME}"
