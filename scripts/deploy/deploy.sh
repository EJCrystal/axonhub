#!/usr/bin/env bash
# Convenience wrapper around bluegreen-deploy.sh.
#
# bluegreen-deploy.sh requires DB_DSN to be provided explicitly so the script
# carries no built-in credentials. This wrapper reuses the DSN that the currently
# running slot container was started with, which keeps deploys one-command while
# still keeping the secret out of the script.
#
# Usage: deploy.sh <image-ref> [version]
set -euo pipefail

IMAGE="${1:?usage: deploy.sh <image-ref> [version]}"
VERSION="${2:-${IMAGE##*:}}"

if [[ -z ${DB_DSN:-} ]]; then
  ACTIVE_SLOT=$(cat /opt/axonhub/deploy-state/active-slot 2>/dev/null || echo '')
  for candidate in "axonhub-slot-$ACTIVE_SLOT" axonhub-slot-a axonhub-slot-b axonhub-app; do
    [[ -z $candidate ]] && continue
    if docker inspect "$candidate" >/dev/null 2>&1; then
      DB_DSN=$(docker inspect "$candidate" --format '{{range .Config.Env}}{{println .}}{{end}}' | sed -n 's/^AXONHUB_DB_DSN=//p' | head -1)
      [[ -n $DB_DSN ]] && break
    fi
  done
fi

if [[ -z ${DB_DSN:-} ]]; then
  echo 'DB_DSN not provided and could not be read from a running container.' >&2
  echo 'Export it explicitly, then retry:' >&2
  echo "  DB_DSN='postgres://user:password@postgres:5432/axonhub?sslmode=disable' $0 $IMAGE $VERSION" >&2
  exit 1
fi

export DB_DSN
exec /opt/axonhub/bluegreen-deploy.sh "$IMAGE" "$VERSION"
