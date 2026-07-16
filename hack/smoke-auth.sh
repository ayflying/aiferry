#!/bin/sh
set -eu

base_url="${1:-http://127.0.0.1:8080}"
container_name="${2:-aiferry-dev-aiferry-1}"
headers="$(mktemp)"
trap 'rm -f "${headers}"' EXIT

health="000"
attempt=1
while [ "${attempt}" -le 10 ]; do
  health="$(curl -sS -o /dev/null -w '%{http_code}' "${base_url}/healthz" || true)"
  [ "${health}" = "200" ] && break
  attempt=$((attempt + 1))
  sleep 2
done

config="$(curl -sS -o /dev/null -w '%{http_code}' "${base_url}/api/auth/config")"
me="$(curl -sS -o /dev/null -w '%{http_code}' "${base_url}/api/auth/me")"
admin="$(curl -sS -o /dev/null -w '%{http_code}' "${base_url}/api/admin/dashboard")"
login="$(curl -sS -D "${headers}" -o /dev/null -w '%{http_code}' "${base_url}/api/auth/login?returnTo=%2Fchannels")"
location="$(awk 'BEGIN{IGNORECASE=1} /^Location:/{sub(/\r$/, "", $2); print $2}' "${headers}")"

state_cookie=no
redirect_target=no
oauth_params=no
runtime_env=no
casdoor_network=no

grep -qi '^Set-Cookie: aiferry_oauth_state=' "${headers}" && state_cookie=yes
case "${location}" in
  https://oidc.luoe.cn/login/oauth/authorize?*) redirect_target=yes ;;
esac
case "${location}" in *client_id=*) client_id_ok=yes ;; *) client_id_ok=no ;; esac
case "${location}" in *response_type=code*) response_type_ok=yes ;; *) response_type_ok=no ;; esac
case "${location}" in *redirect_uri=*) redirect_uri_ok=yes ;; *) redirect_uri_ok=no ;; esac
case "${location}" in *state=*) state_ok=yes ;; *) state_ok=no ;; esac
[ "${client_id_ok}" = "yes" ] && [ "${response_type_ok}" = "yes" ] && [ "${redirect_uri_ok}" = "yes" ] && [ "${state_ok}" = "yes" ] && oauth_params=yes
docker exec "${container_name}" sh -c 'test -n "$CASDOOR_ENDPOINT" && test -n "$CASDOOR_CLIENT_ID" && test -n "$CASDOOR_CLIENT_SECRET"' && runtime_env=yes
docker exec "${container_name}" sh -c 'wget -q -T 15 -O /dev/null "$CASDOOR_ENDPOINT/"' && casdoor_network=yes

printf 'health=%s config=%s me=%s admin=%s login=%s state_cookie=%s redirect_target=%s oauth_params=%s runtime_env=%s casdoor_network=%s\n' \
  "${health}" "${config}" "${me}" "${admin}" "${login}" "${state_cookie}" "${redirect_target}" "${oauth_params}" "${runtime_env}" "${casdoor_network}"

[ "${health}" = "200" ]
[ "${config}" = "200" ]
[ "${me}" = "401" ]
[ "${admin}" = "401" ]
[ "${login}" = "302" ]
[ "${state_cookie}" = "yes" ]
[ "${redirect_target}" = "yes" ]
[ "${oauth_params}" = "yes" ]
[ "${runtime_env}" = "yes" ]
[ "${casdoor_network}" = "yes" ]
