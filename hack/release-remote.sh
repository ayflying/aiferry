#!/bin/sh
set -eu

version="${1:?version is required}"
image="${2:?image is required}"
deploy_dir="${3:?deploy directory is required}"
compose_project="${4:?compose project is required}"
revision="${5:?revision is required}"

case "$version" in
  *[!0-9.]*|*.*.*.*|.*|*.) echo "invalid version: $version" >&2; exit 2 ;;
esac

if [ "$(tr -d '\r\n' < VERSION)" != "$version" ]; then
  echo "VERSION does not match requested release version" >&2
  exit 2
fi
if [ ! -f "$deploy_dir/.env" ]; then
  echo "deployment .env is missing: $deploy_dir/.env" >&2
  exit 2
fi
if [ ! -f "$HOME/.docker/config.json" ] || \
  { ! grep -q 'ghcr.io' "$HOME/.docker/config.json" && ! grep -q '"credsStore"' "$HOME/.docker/config.json"; }; then
  echo "GHCR credentials are missing. Run: docker login ghcr.io -u ayflying" >&2
  exit 3
fi

upsert_env() {
  key="$1"
  value="$2"
  env_file="$3"
  temp_file="$(mktemp "${env_file}.XXXXXX")"
  awk -v key="$key" -v value="$value" '
    BEGIN { found = 0 }
    index($0, key "=") == 1 { print key "=" value; found = 1; next }
    { print }
    END { if (!found) print key "=" value }
  ' "$env_file" > "$temp_file"
  mv "$temp_file" "$env_file"
}

echo "Building $image:$version on the remote build server"
docker build \
  --build-arg "VERSION=$version" \
  --build-arg "VCS_REF=$revision" \
  --tag "$image:$version" \
  --tag "$image:latest" \
  .

echo "Pushing $image:$version and $image:latest"
docker push "$image:$version"
docker push "$image:latest"

install -d "$deploy_dir"
cp docker-compose.yml "$deploy_dir/docker-compose.yml"
upsert_env AIFERRY_IMAGE "$image" "$deploy_dir/.env"
upsert_env AIFERRY_VERSION "$version" "$deploy_dir/.env"

echo "Pulling the published image through Docker Compose"
docker compose --project-name "$compose_project" -f "$deploy_dir/docker-compose.yml" config --quiet
docker compose --project-name "$compose_project" -f "$deploy_dir/docker-compose.yml" pull aiferry
docker compose --project-name "$compose_project" -f "$deploy_dir/docker-compose.yml" up -d --no-deps --force-recreate aiferry

for attempt in $(seq 1 24); do
  container_id="$(docker compose --project-name "$compose_project" -f "$deploy_dir/docker-compose.yml" ps -q aiferry)"
  if [ -n "$container_id" ]; then
    health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$container_id")"
    if [ "$health" = "healthy" ]; then
      echo "Release $version is healthy"
      exit 0
    fi
    if [ "$health" = "unhealthy" ]; then
      echo "Release $version became unhealthy" >&2
      exit 1
    fi
  fi
  sleep 5
done

echo "Release $version did not become healthy in time" >&2
exit 1
