#!/bin/sh
set -eu

mysql_dsn="${MYSQL_USER}:${MYSQL_PASSWORD}@tcp(${MYSQL_HOST}:${MYSQL_PORT:-3306})/${MYSQL_DATABASE}?parseTime=true&charset=utf8mb4"
gf_link="mysql:${mysql_dsn}"

case "${1:-}" in
  migrate)
    go run github.com/pressly/goose/v3/cmd/goose@v3.25.0 \
      -dir manifest/migrations mysql "${mysql_dsn}" up
    ;;
  dao)
    gf gen dao -l "${gf_link}" -p internal -c -s
    ;;
  tidy)
    go mod tidy
    ;;
  format)
    gofmt -w api internal main.go
    ;;
  backend-test)
    go test ./...
    ;;
  frontend-install)
    cd frontend
    if [ -f package-lock.json ]; then npm ci; else npm install; fi
    ;;
  frontend-update)
    cd frontend
    npm install
    ;;
  frontend-check)
    cd frontend
    npm run typecheck
    npm run test:run
    npm run build
    ;;
  acceptance-cleanup)
    export MYSQL_PWD="${MYSQL_PASSWORD}"
    mysql --protocol=tcp --host="${MYSQL_HOST}" --port="${MYSQL_PORT:-3306}" --user="${MYSQL_USER}" "${MYSQL_DATABASE}" <<'SQL'
DELETE u FROM usage_logs u INNER JOIN api_keys k ON k.id=u.api_key_id WHERE k.name LIKE 'acceptance-%';
DELETE s FROM channel_cost_snapshots s INNER JOIN channels c ON c.id=s.channel_id WHERE c.name LIKE 'acceptance-%';
DELETE m FROM channel_models m INNER JOIN channels c ON c.id=m.channel_id WHERE c.name LIKE 'acceptance-%';
DELETE FROM channels WHERE name LIKE 'acceptance-%';
DELETE FROM api_keys WHERE name LIKE 'acceptance-%';
SQL
    ;;
  *)
    echo "usage: $0 {migrate|dao|tidy|format|backend-test|frontend-install|frontend-update|frontend-check|acceptance-cleanup}" >&2
    exit 2
    ;;
esac
