#!/usr/bin/env bash
set -euo pipefail

# Runs the browser end-to-end suite against a real VisitFlow server that embeds
# the real `npm run build` output, the same way the CI `e2e` job does. The
# server hardcodes `:8080` (cmd/visitflow/main.go), so it runs in a container on
# a private network and the host port is assigned by the OS — nothing here binds
# a fixed port, and nothing touches containers, networks or images it did not
# create itself.
#
# "registers a site, a lobby and an organization from the admin console" has
# failed against hand-assembled local servers while passing in CI; it passes
# here, because every run gets a fresh database and a fresh server. The suite's
# exit code is reported verbatim either way and no spec is ever skipped.

if [[ $# -ne 0 ]]; then
  echo "usage: $0" >&2
  echo "  VISITFLOW_LOCAL_E2E_PORT      host port for the server (default: assigned by the OS)" >&2
  echo "  VISITFLOW_E2E_BROWSER_CHANNEL Playwright browser channel (default: auto-detected)" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

for binary in docker node npm go curl; do
  if ! command -v "$binary" > /dev/null 2>&1; then
    echo "$binary is required but was not found in PATH" >&2
    exit 1
  fi
done

web_dist="$repo_root/cmd/visitflow/webdist"
work_dir="$(mktemp -d)"
# Unique so that concurrent runs, and the unrelated containers already on this
# host, cannot collide with or be removed by this one.
prefix="vf-local-e2e-$$-${RANDOM}"
network_created=""
started_containers=()

cleanup() {
  local keep_status=$?
  set +e
  # Restore the tracked webdist stub from the temporary copy first. Using
  # `git checkout --` here has cost uncommitted work in this repository before.
  if [[ -d "$work_dir/webdist-backup" ]]; then
    rm -rf "$web_dist"
    cp -a "$work_dir/webdist-backup" "$web_dist"
  fi
  local container
  for container in "${started_containers[@]+"${started_containers[@]}"}"; do
    docker rm --force --volumes "$container" > /dev/null 2>&1
  done
  if [[ -n "$network_created" ]]; then
    docker network rm "$network_created" > /dev/null 2>&1
  fi
  rm -rf "$work_dir"
  return "$keep_status"
}
trap cleanup EXIT INT TERM

echo "==> web dependencies"
if [[ ! -d "$repo_root/web/node_modules" ]]; then
  (cd "$repo_root/web" && npm ci)
fi

echo "==> production UI build"
(cd "$repo_root/web" && npm run build)

echo "==> embedding the build into cmd/visitflow/webdist"
cp -a "$web_dist" "$work_dir/webdist-backup"
rm -rf "$web_dist"
mkdir -p "$web_dist"
cp -r "$repo_root/web/dist/." "$web_dist/"

echo "==> server build"
CGO_ENABLED=0 go build -o "$work_dir/visitflow" ./cmd/visitflow

echo "==> PostgreSQL"
network_created="$prefix-net"
docker network create "$network_created" > /dev/null
started_containers+=("$prefix-pg")
docker run --detach \
  --name "$prefix-pg" \
  --network "$network_created" \
  --network-alias postgres \
  --env POSTGRES_USER=visitflow \
  --env POSTGRES_PASSWORD=visitflow \
  --env POSTGRES_DB=visitflow \
  postgres:16-alpine > /dev/null
for _ in $(seq 1 60); do
  if docker exec "$prefix-pg" pg_isready --username visitflow > /dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! docker exec "$prefix-pg" pg_isready --username visitflow > /dev/null 2>&1; then
  echo "PostgreSQL did not become ready" >&2
  docker logs "$prefix-pg" >&2 || true
  exit 1
fi

echo "==> VisitFlow server"
# An empty host port lets the OS pick a free one; 8080 is routinely taken by
# another project on a developer machine.
host_port_spec="127.0.0.1:${VISITFLOW_LOCAL_E2E_PORT-}:8080"
admin_user="${VISITFLOW_E2E_ADMIN:-admin}"
admin_password="${VISITFLOW_E2E_PASSWORD:-e2e-bootstrap-password}"
started_containers+=("$prefix-app")
docker run --detach \
  --name "$prefix-app" \
  --network "$network_created" \
  --publish "$host_port_spec" \
  --volume "$work_dir/visitflow:/usr/local/bin/visitflow:ro" \
  --env "POSTGRES_DSN=postgres://visitflow:visitflow@postgres:5432/visitflow?sslmode=disable" \
  --env "BOOTSTRAP_ADMIN=$admin_user" \
  --env "BOOTSTRAP_ADMIN_PASSWORD=$admin_password" \
  --env "ENCRYPTION_KEY=$(node -e 'process.stdout.write(require("crypto").randomBytes(32).toString("base64"))')" \
  debian:12-slim /usr/local/bin/visitflow > /dev/null

host_port="$(docker port "$prefix-app" 8080/tcp | head -1)"
host_port="${host_port##*:}"
if [[ -z "$host_port" ]]; then
  echo "could not resolve the published host port" >&2
  exit 1
fi
base_url="http://127.0.0.1:$host_port"

for _ in $(seq 1 60); do
  if curl --fail --silent --show-error "$base_url/readyz" > /dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! curl --fail --silent --show-error "$base_url/readyz" > /dev/null 2>&1; then
  echo "VisitFlow did not become ready at $base_url" >&2
  docker logs "$prefix-app" >&2 || true
  exit 1
fi

echo "==> browser"
browser_note="Playwright's bundled chromium"
if [[ -n "${VISITFLOW_E2E_BROWSER_CHANNEL+set}" ]]; then
  browser_note="channel '${VISITFLOW_E2E_BROWSER_CHANNEL}' (from the environment)"
elif (cd "$repo_root/web" && node -e '
  require("@playwright/test").chromium.launch()
    .then((browser) => browser.close())
    .catch(() => process.exit(1));
' > /dev/null 2>&1); then
  : # The bundled browser is installed, so behave exactly like CI.
elif command -v google-chrome > /dev/null 2>&1; then
  # No bundled download on this machine, but a system Chrome is installed.
  export VISITFLOW_E2E_BROWSER_CHANNEL=chrome
  browser_note="channel 'chrome' (no bundled browser installed)"
else
  (cd "$repo_root/web" && npx playwright install chromium)
fi
echo "using $browser_note"

echo "==> npm run test:e2e against $base_url"
suite_status=0
(cd "$repo_root/web" && VISITFLOW_BASE_URL="$base_url" VISITFLOW_E2E_ADMIN="$admin_user" \
  VISITFLOW_E2E_PASSWORD="$admin_password" npm run test:e2e) || suite_status=$?

if [[ "$suite_status" -eq 0 ]]; then
  echo "playwright: passed (base URL $base_url, $browser_note)"
else
  echo "playwright: failed with exit code $suite_status (base URL $base_url, $browser_note)"
fi
exit "$suite_status"
