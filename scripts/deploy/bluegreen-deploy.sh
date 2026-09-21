#!/usr/bin/env bash
# AxonHub zero-downtime (blue-green) deploy.
#
# Why: `docker compose up -d` recreates the axonhub container in place. While it is
# recreated nothing listens on the published port, so nginx answers 502 and
# Cloudflare shows a connection timeout.
#
# How: boot the new image in an idle slot (18090/18091), wait for it to report
# healthy, repoint the nginx upstream, and only then retire the previous container.
#
# The subtlety: `nginx -s reload` does not mutate a running worker's config. Old
# workers keep serving their existing keepalive sockets (Cloudflare holds HTTP/2
# connections) against the OLD upstream port, so the old container must stay
# reachable until those workers exit. `worker_shutdown_timeout` bounds that exit.
# The switch therefore runs in two phases:
#   1. upstream = new primary + old backup; wait for pre-reload workers to drain.
#   2. upstream = new only; wait for the phase-1 workers to drain, then remove the
#      old container.
#
# Prerequisite (already applied on the host):
#   - /etc/nginx/conf.d/tokenshub.conf proxies axonhub.lumior.vip to the
#     `axonhub_backend` upstream instead of a hard-coded 127.0.0.1:8090.
#   - /etc/nginx/nginx.conf sets `worker_shutdown_timeout 30s;` in main context.
#   - postgres keeps running as the compose `axonhub-postgres` container.
#
# Usage: bluegreen-deploy.sh <image-ref> [version]
set -euo pipefail

IMAGE="${1:?usage: bluegreen-deploy.sh <image-ref> [version]}"
VERSION="${2:-${IMAGE##*:}}"

NETWORK=${NETWORK:-axonhub_axonhub-network}
STATE_DIR=${STATE_DIR:-/opt/axonhub/deploy-state}
UPSTREAM_CONF=${UPSTREAM_CONF:-/etc/nginx/conf.d/00-axonhub-upstream.conf}
ASSETS_UPSTREAM_CONF=${ASSETS_UPSTREAM_CONF:-/etc/nginx/conf.d/01-axonhub-assets-upstream.conf}
CONFIG_MOUNT=${CONFIG_MOUNT:-/opt/axonhub/config.yml}
DB_DSN=${DB_DSN:-postgres://axonhub:axonhub_password@postgres:5432/axonhub?sslmode=disable}
PUBLIC_HEALTH_URL=${PUBLIC_HEALTH_URL:-https://axonhub.lumior.vip/health}
SLOT_A_PORT=${SLOT_A_PORT:-18090}
SLOT_B_PORT=${SLOT_B_PORT:-18091}
DRAIN_TIMEOUT=${DRAIN_TIMEOUT:-180}

ACTIVE_FILE="$STATE_DIR/active-slot"
LOCK_FILE="$STATE_DIR/deploy.lock"
BACKUP_DIR=${BACKUP_DIR:-/opt/axonhub/backups/$(date -u +%Y%m%dT%H%M%SZ)-bluegreen}

port_for() { case "$1" in a) echo "$SLOT_A_PORT";; b) echo "$SLOT_B_PORT";; *) return 1;; esac; }
slot_for_port() { case "$1" in "$SLOT_A_PORT") echo a;; "$SLOT_B_PORT") echo b;; *) return 1;; esac; }
name_for() { echo "axonhub-slot-$1"; }

mkdir -p "$STATE_DIR" "$BACKUP_DIR"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then echo 'another deploy is running' >&2; exit 1; fi

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
inspect_state() { docker inspect -f '{{.State.Health.Status}}' "$1" 2>/dev/null || echo missing; }
container_running() { [[ $(docker inspect -f '{{.State.Running}}' "$1" 2>/dev/null) == true ]]; }
nginx_workers() { pgrep -f 'nginx: worker process' 2>/dev/null | sort -n | tr '\n' ' '; }

wait_healthy() {
  local name=$1 port=$2 deadline=$((SECONDS + 180)) status restarts
  while (( SECONDS < deadline )); do
    if [[ $(inspect_state "$name") == healthy ]] && curl -fsS -m 5 "http://127.0.0.1:$port/health" >/dev/null 2>&1; then
      return 0
    fi
    status=$(docker inspect -f '{{.State.Status}}' "$name" 2>/dev/null || echo gone)
    restarts=$(docker inspect -f '{{.RestartCount}}' "$name" 2>/dev/null || echo 0)
    if [[ $status == gone || $status == exited || $status == dead ]]; then
      echo "container $name is $status" >&2; docker logs --tail 40 "$name" >&2 || true; return 1
    fi
    if [[ $status == restarting || $restarts -ge 3 ]]; then
      echo "container $name is crash-looping (status=$status restarts=$restarts)" >&2
      docker logs --tail 40 "$name" >&2 || true; return 1
    fi
    sleep 3
  done
  echo "timed out waiting for $name" >&2; return 1
}

point_upstream() {
  local primary=$1 backup=${2:-} tmp
  tmp=$(mktemp)
  {
    echo '# Managed by bluegreen-deploy.sh - do not edit by hand.'
    echo 'upstream axonhub_backend {'
    echo "    server 127.0.0.1:$primary;"
    if [[ -n $backup ]]; then echo "    server 127.0.0.1:$backup backup;"; fi
    echo '}'
  } > "$tmp"
  install -m 0644 "$tmp" "$UPSTREAM_CONF"
  rm -f "$tmp"
  nginx -t >/dev/null 2>&1
  nginx -s reload
}

# Assets upstream lists the live slot first and the previous slot as a backup, so a
# tab that still runs the previous build can fetch chunks the new build does not have.
# Only writes the file; the caller triggers a single reload.
point_assets_upstream() {
  local primary=$1 backup=${2:-} tmp
  # A `backup` server that is down turns a plain 404 into a 502, so only advertise
  # the fallback while its container is actually running.
  if [[ -n $backup ]] && ! container_running "$(name_for "$(slot_for_port "$backup")")"; then backup=''; fi
  tmp=$(mktemp)
  {
    echo '# Managed by bluegreen-deploy.sh - do not edit by hand.'
    echo 'upstream axonhub_assets {'
    echo "    server 127.0.0.1:$primary;"
    if [[ -n $backup ]]; then echo "    server 127.0.0.1:$backup backup;"; fi
    echo '}'
  } > "$tmp"
  install -m 0644 "$tmp" "$ASSETS_UPSTREAM_CONF"
  rm -f "$tmp"
}

wait_drain() {
  local pids=$1 deadline=$((SECONDS + DRAIN_TIMEOUT)) pid alive
  while (( SECONDS < deadline )); do
    alive=0
    for pid in $pids; do
      if kill -0 "$pid" 2>/dev/null; then alive=1; break; fi
    done
    (( alive == 0 )) && return 0
    sleep 2
  done
  return 1
}

domain_ok() { curl -fsS -o /dev/null -m 20 "$PUBLIC_HEALTH_URL"; }

docker pull "$IMAGE" >/dev/null
DIGEST=$(docker image inspect "$IMAGE" --format '{{index .RepoDigests 0}}')
IMAGE_ID=$(docker image inspect "$IMAGE" --format '{{.Id}}')
log "image $IMAGE -> $DIGEST"

ACTIVE_SLOT=$(cat "$ACTIVE_FILE" 2>/dev/null || echo '')
if [[ "$ACTIVE_SLOT" != a && "$ACTIVE_SLOT" != b ]]; then ACTIVE_SLOT=''; fi
if [[ -n $ACTIVE_SLOT ]]; then
  TARGET_SLOT=$([[ $ACTIVE_SLOT == a ]] && echo b || echo a)
else
  TARGET_SLOT=a
  ACTIVE_SLOT=none
fi
TARGET_PORT=$(port_for "$TARGET_SLOT")
TARGET_NAME=$(name_for "$TARGET_SLOT")
ACTIVE_PORT=''
if [[ $ACTIVE_SLOT != none ]]; then ACTIVE_PORT=$(port_for "$ACTIVE_SLOT"); fi
log "active=$ACTIVE_SLOT target=$TARGET_SLOT port=$TARGET_PORT"

cp -a "$UPSTREAM_CONF" "$BACKUP_DIR/upstream-before.conf"
cp -a "$ASSETS_UPSTREAM_CONF" "$BACKUP_DIR/assets-upstream-before.conf" 2>/dev/null || true
if [[ $ACTIVE_SLOT != none ]]; then echo "$ACTIVE_SLOT" > "$BACKUP_DIR/active-slot-before"; fi
docker rm -f "$TARGET_NAME" >/dev/null 2>&1 || true

# 1. boot the new image in the idle slot; the live slot keeps serving.
docker run -d \
  --name "$TARGET_NAME" \
  --network "$NETWORK" \
  -p "0.0.0.0:$TARGET_PORT:8090" \
  --restart unless-stopped \
  -e AXONHUB_DB_DIALECT=postgres \
  -e "AXONHUB_DB_DSN=$DB_DSN" \
  -v "$CONFIG_MOUNT:/app/config.yml:ro" \
  --health-cmd 'wget --no-verbose --tries=1 --spider http://localhost:8090/health' \
  --health-interval 10s --health-timeout 10s --health-retries 3 --health-start-period 20s \
  "$DIGEST" >/dev/null
log "started $TARGET_NAME on port $TARGET_PORT"

ROLLED_BACK=0
rollback() {
  (( ROLLED_BACK )) && return 0
  ROLLED_BACK=1
  log 'deploy failed - rolling back'
  if [[ -f "$BACKUP_DIR/upstream-before.conf" ]]; then
    install -m 0644 "$BACKUP_DIR/upstream-before.conf" "$UPSTREAM_CONF"
    if [[ -f "$BACKUP_DIR/assets-upstream-before.conf" ]]; then
      install -m 0644 "$BACKUP_DIR/assets-upstream-before.conf" "$ASSETS_UPSTREAM_CONF"
    fi
    nginx -t >/dev/null 2>&1 && nginx -s reload || true
  fi
  if [[ -f "$BACKUP_DIR/active-slot-before" ]]; then
    cp -a "$BACKUP_DIR/active-slot-before" "$ACTIVE_FILE"
  else
    rm -f "$ACTIVE_FILE"
  fi
  docker rm -f "$TARGET_NAME" >/dev/null 2>&1 || true
}
trap 'rc=$?; if (( rc != 0 )); then rollback; fi' EXIT

# 2. the candidate must be healthy before it receives any traffic.
wait_healthy "$TARGET_NAME" "$TARGET_PORT"
curl -fsS -m 10 "http://127.0.0.1:$TARGET_PORT/health" > "$BACKUP_DIR/candidate-health.json"
log "candidate healthy: $(cat "$BACKUP_DIR/candidate-health.json")"

# 3. phase 1: new slot becomes primary, old slot stays as backup so workers that
#    were forked before this reload can still reach it.
PRE_RELOAD_WORKERS=$(nginx_workers)
point_assets_upstream "$TARGET_PORT" "$ACTIVE_PORT"
point_upstream "$TARGET_PORT" "$ACTIVE_PORT"
sleep 1
if ! domain_ok; then echo 'public health check failed after switch' >&2; exit 1; fi
echo "$TARGET_SLOT" > "$ACTIVE_FILE"
log "traffic switched to $TARGET_NAME (pre-reload workers: ${PRE_RELOAD_WORKERS:-none})"

DRAINED=1
if [[ $ACTIVE_SLOT != none ]]; then
  if wait_drain "$PRE_RELOAD_WORKERS"; then
    log 'pre-reload workers drained'
  else
    DRAINED=0
    log "WARN: pre-reload workers still alive after ${DRAIN_TIMEOUT}s; leaving the old container in place"
  fi
fi

# 4. phase 2: collapse the upstream, drain those workers, then retire the old slot.
if (( DRAINED )) && [[ $ACTIVE_SLOT != none ]]; then
  PHASE1_WORKERS=$(nginx_workers)
  point_upstream "$TARGET_PORT"
  log 'upstream collapsed to the new slot'
  if wait_drain "$PHASE1_WORKERS"; then
    # Keep the previous slot running: it no longer receives traffic, but
    # /assets/ falls back to it so tabs on the previous build still load.
    log "previous slot $ACTIVE_SLOT kept for asset fallback"
  else
    DRAINED=0
    log 'WARN: phase-1 workers still alive; leaving the old container in place'
  fi
fi

if (( DRAINED )) && docker inspect axonhub-app >/dev/null 2>&1; then
  # Legacy compose container: no longer receiving traffic, so reclaim the port it
  # held. Its image stays in the local cache for rollback.
  docker stop axonhub-app >/dev/null 2>&1 || true
  docker rm axonhub-app >/dev/null 2>&1 || true
  log 'retired legacy axonhub-app container'
fi

cat > "$STATE_DIR/current.json" <<JSON
{
  "version": "$VERSION",
  "image": "$IMAGE",
  "digest": "$DIGEST",
  "imageId": "$IMAGE_ID",
  "slot": "$TARGET_SLOT",
  "container": "$TARGET_NAME",
  "port": $TARGET_PORT,
  "deployedAt": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "drained": $DRAINED
}
JSON

trap - EXIT
log "DEPLOY_OK version=$VERSION slot=$TARGET_SLOT container=$TARGET_NAME port=$TARGET_PORT drained=$DRAINED"
cat "$STATE_DIR/current.json"
