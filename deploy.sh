#!/usr/bin/env bash
# deploy.sh - Production deployment script for nginx-3x-ui-subscription-proxy
set -euo pipefail

APP_NAME="nginx-3x-ui-subscription-proxy"
APP_DIR="/docker/${APP_NAME}"
LOG_FILE="/var/log/${APP_NAME}_deploy.log"

log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

cleanup() {
  local exit_code=$?
  if [ $exit_code -ne 0 ]; then
    log "ERROR: Deployment failed with exit code $exit_code"
    log "Rolling back to $CURRENT_COMMIT..."
    git checkout "$CURRENT_COMMIT" || true
    docker compose pull
    docker compose up -d
  fi
  exit $exit_code
}
trap cleanup EXIT

log "Starting deployment..."

cd "$APP_DIR"

# Save current state
CURRENT_COMMIT=$(git rev-parse HEAD 2>/dev/null || echo "none")
log "Current commit: $CURRENT_COMMIT"

# Pull latest code
log "Pulling latest code..."
git fetch origin
git checkout main
git pull origin main

# Update .env if provided
if [ -n "${ENV_FILE_BASE64:-}" ]; then
  log "Updating .env file..."
  echo "$ENV_FILE_BASE64" | base64 -d > .env
fi

# Pull latest images
log "Pulling Docker images..."
if ! docker compose pull 2>&1 | tee -a "$LOG_FILE"; then
  log "ERROR: Failed to pull images. Rolling back..."
  exit 1
fi

# Restart with cleanup
log "Restarting services..."
if ! docker compose up -d --remove-orphans 2>&1 | tee -a "$LOG_FILE"; then
  log "ERROR: Failed to start containers. Rolling back..."
  exit 1
fi

# Health check
log "Running health checks..."
HEALTH_CHECK_RETRIES=30
HEALTH_CHECK_INTERVAL=10
HEALTH_CHECK_PASSED=false

for ((i=1; i<=HEALTH_CHECK_RETRIES; i++)); do
  log "Health check attempt $i/$HEALTH_CHECK_RETRIES..."
  if docker compose exec -T python curl -sf http://localhost:8080/health > /dev/null 2>&1; then
    log "Health check passed!"
    HEALTH_CHECK_PASSED=true
    break
  fi
  sleep $HEALTH_CHECK_INTERVAL
done

if [ "$HEALTH_CHECK_PASSED" = false ]; then
  log "ERROR: Health check failed after $HEALTH_CHECK_RETRIES attempts"
  exit 1
fi

# Cleanup
log "Cleaning up old images..."
docker image prune -af --filter "until=24h"

log "Deployment completed successfully!"
