#!/usr/bin/env bash

set -euo pipefail

DEPLOY_DIR="${DEPLOY_DIR:-/opt/reminder}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"

if [ "$(id -u)" -eq 0 ]; then
  SUDO=()
else
  SUDO=(sudo)
fi

log() {
  printf '[deploy] %s\n' "$*"
}

install_docker() {
  if command -v docker >/dev/null 2>&1; then
    return
  fi

  log "Docker not found. Installing Docker Engine."

  if command -v apt-get >/dev/null 2>&1; then
    "${SUDO[@]}" apt-get update
    "${SUDO[@]}" apt-get install -y ca-certificates curl
    curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
    "${SUDO[@]}" sh /tmp/get-docker.sh
  else
    log "Unsupported package manager. Install Docker and the compose plugin manually, then rerun this script."
    exit 1
  fi
}

require_compose() {
  if docker compose version >/dev/null 2>&1; then
    return
  fi

  log "Docker Compose plugin not found. Installing docker-compose-plugin."

  if command -v apt-get >/dev/null 2>&1; then
    "${SUDO[@]}" apt-get update
    "${SUDO[@]}" apt-get install -y docker-compose-plugin
  fi

  if ! docker compose version >/dev/null 2>&1; then
    log "Docker Compose plugin is unavailable after installation."
    exit 1
  fi
}

main() {
  install_docker
  require_compose

  cd "$DEPLOY_DIR"

  if [ ! -f ".env" ]; then
    log ".env is missing in $DEPLOY_DIR."
    exit 1
  fi

  mkdir -p data

  log "Validating production compose file."
  docker compose -f "$COMPOSE_FILE" config >/dev/null

  log "Starting production stack."
  docker compose -f "$COMPOSE_FILE" up -d --build

  log "Waiting for local health check."
  for attempt in $(seq 1 30); do
    if docker compose -f "$COMPOSE_FILE" exec -T reminder sh -c 'wget -q -O - "http://127.0.0.1${APP_ADDR}/health" >/dev/null'; then
      log "Application is healthy."
      docker compose -f "$COMPOSE_FILE" ps
      exit 0
    fi
    sleep 2
    log "Health check attempt $attempt failed; retrying."
  done

  log "Application did not become healthy in time."
  docker compose -f "$COMPOSE_FILE" ps
  docker compose -f "$COMPOSE_FILE" logs --tail=100 reminder
  exit 1
}

main "$@"
