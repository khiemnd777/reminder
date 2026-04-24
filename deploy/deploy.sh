#!/usr/bin/env bash

set -euo pipefail

DEPLOY_DIR="${DEPLOY_DIR:-/opt/reminder}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
DOMAIN="${DOMAIN:-manle.info}"

if [ "$(id -u)" -eq 0 ]; then
  SUDO=()
else
  SUDO=(sudo)
fi

log() {
  printf '[deploy] %s\n' "$*"
}

derive_app_port() {
  if [ -z "${APP_ADDR:-}" ]; then
    log "APP_ADDR is missing."
    exit 1
  fi

  APP_PORT="${APP_ADDR##*:}"

  case "$APP_PORT" in
    ''|*[!0-9]*)
      log "APP_ADDR must end with a numeric port; got '${APP_ADDR}'."
      exit 1
      ;;
  esac

  export APP_PORT
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

find_existing_caddy_container() {
  for container_id in $(docker ps --filter "name=caddy" --format '{{.ID}}'); do
    caddyfile_path="$(docker inspect "$container_id" --format '{{range .Mounts}}{{if eq .Destination "/etc/caddy/Caddyfile"}}{{.Source}}{{end}}{{end}}')"
    if [ -n "$caddyfile_path" ] && [ -f "$caddyfile_path" ]; then
      printf '%s:%s\n' "$container_id" "$caddyfile_path"
      return 0
    fi
  done

  return 1
}

host_web_ports_are_busy() {
  if command -v ss >/dev/null 2>&1; then
    ss -ltnp 2>/dev/null | grep -Eq ':(80|443)[[:space:]]'
    return
  fi

  if command -v netstat >/dev/null 2>&1; then
    netstat -ltnp 2>/dev/null | grep -Eq ':(80|443)[[:space:]]'
    return
  fi

  return 1
}

configure_existing_caddy() {
  existing_caddy="$1"
  caddy_container="${existing_caddy%%:*}"
  caddyfile_path="${existing_caddy#*:}"
  backup_path="${caddyfile_path}.bak.$(date +%Y%m%d%H%M%S)"
  temp_path="$(mktemp)"
  start_marker="# reminder managed start"
  end_marker="# reminder managed end"

  log "Configuring existing Caddy container ${caddy_container} with ${DOMAIN} -> 127.0.0.1:${APP_PORT}."

  cp "$caddyfile_path" "$backup_path"
  awk -v start="$start_marker" -v end="$end_marker" '
    $0 == start { skip = 1; next }
    $0 == end { skip = 0; next }
    !skip { print }
  ' "$caddyfile_path" > "$temp_path"

  cat >> "$temp_path" <<EOF

${start_marker}
${DOMAIN} {
	encode zstd gzip
	reverse_proxy 127.0.0.1:${APP_PORT}
}
${end_marker}
EOF
  cat "$temp_path" > "$caddyfile_path"
  rm -f "$temp_path"

  if docker exec "$caddy_container" caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile && docker exec "$caddy_container" grep -q "$DOMAIN" /etc/caddy/Caddyfile; then
    log "Existing Caddy reloaded successfully."
    return 0
  fi

  log "Existing Caddy did not pick up the updated Caddyfile; recreating the Caddy container to remount it."
  if recreate_existing_caddy "$caddy_container" && existing_caddy="$(find_existing_caddy_container)" && docker exec "${existing_caddy%%:*}" grep -q "$DOMAIN" /etc/caddy/Caddyfile; then
    log "Existing Caddy remounted and started successfully."
    return 0
  fi

  log "Existing Caddy update failed. Restoring previous Caddyfile."
  cp "$backup_path" "$caddyfile_path"
  recreate_existing_caddy "$caddy_container" || docker exec "$caddy_container" caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile || true
  exit 1
}

recreate_existing_caddy() {
  caddy_container="$1"
  compose_project="$(docker inspect "$caddy_container" --format '{{ index .Config.Labels "com.docker.compose.project" }}')"
  compose_workdir="$(docker inspect "$caddy_container" --format '{{ index .Config.Labels "com.docker.compose.project.working_dir" }}')"
  compose_files="$(docker inspect "$caddy_container" --format '{{ index .Config.Labels "com.docker.compose.project.config_files" }}')"
  compose_service="$(docker inspect "$caddy_container" --format '{{ index .Config.Labels "com.docker.compose.service" }}')"

  if [ -z "$compose_project" ] || [ -z "$compose_workdir" ] || [ -z "$compose_files" ] || [ -z "$compose_service" ]; then
    return 1
  fi

  log "Recreating existing Caddy service ${compose_project}/${compose_service} from ${compose_workdir}."
  (
    cd "$compose_workdir"
    docker compose -p "$compose_project" -f "$compose_files" up -d --force-recreate --no-deps "$compose_service"
  )
}

main() {
  install_docker
  require_compose
  derive_app_port

  cd "$DEPLOY_DIR"

  if [ ! -f ".env" ]; then
    log ".env is missing in $DEPLOY_DIR."
    exit 1
  fi

  mkdir -p data

  log "Validating production compose file."
  docker compose --profile standalone -f "$COMPOSE_FILE" config >/dev/null

  existing_caddy=""
  if host_web_ports_are_busy; then
    if existing_caddy="$(find_existing_caddy_container)"; then
      log "Host web ports are already owned by an existing Caddy. Starting only the app container."
      docker compose -f "$COMPOSE_FILE" up -d --build reminder
      configure_existing_caddy "$existing_caddy"
    else
      log "Host ports 80/443 are busy, but no reusable Caddy container was found."
      exit 1
    fi
  else
    log "Starting production stack with standalone Caddy."
    docker compose --profile standalone -f "$COMPOSE_FILE" up -d --build
  fi

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
