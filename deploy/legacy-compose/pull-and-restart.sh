#!/usr/bin/env bash
set -euo pipefail

deploy_dir="${DEPLOY_DIR:-/opt/live-auction-backend}"
image_tag="${1:-${IMAGE_TAG:-latest}}"

if [[ ! "$image_tag" =~ ^[A-Za-z0-9._-]{1,128}$ ]]; then
  echo "invalid image tag: $image_tag" >&2
  exit 1
fi

cd "$deploy_dir"

if [ ! -f config.yaml ]; then
  echo "missing config.yaml in $deploy_dir" >&2
  exit 1
fi

if [ ! -f docker-compose.prod.yml ]; then
  echo "missing docker-compose.prod.yml in $deploy_dir" >&2
  exit 1
fi

old_env="$(mktemp)"
tmp_env="$(mktemp)"
if [ -f .env ]; then
  cp .env "$old_env"
  grep -v '^IMAGE_TAG=' .env > "$tmp_env" || true
fi
printf 'IMAGE_TAG=%s\n' "$image_tag" >> "$tmp_env"
mv "$tmp_env" .env

restore_env() {
  if [ -s "$old_env" ]; then
    mv "$old_env" .env
  fi
}
trap restore_env ERR

for attempt in 1 2 3; do
  if docker compose -f docker-compose.prod.yml pull app; then
    break
  fi
  if [ "$attempt" -eq 3 ]; then
    echo "failed to pull app image after $attempt attempts" >&2
    restore_env
    exit 1
  fi
  sleep $((attempt * 5))
done

trap - ERR
rm -f "$old_env"
docker compose -f docker-compose.prod.yml up -d --remove-orphans
docker image prune -f
