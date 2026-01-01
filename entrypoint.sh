#!/bin/sh
set -e

# Запускаем cron в фоне
crond -l 2

# Добавляем дефолтные сертификаты, если нет подходящих
CERT_DIR="/etc/letsencrypt/live/$SITE_HOST"
CERT_PATH="$CERT_DIR/fullchain.pem"
KEY_PATH="$CERT_DIR/privkey.pem"
if [ ! -f "$CERT_PATH" ] || [ ! -f "$KEY_PATH" ]; then
    echo "Generating default certificate..."
    mkdir -p "$CERT_DIR"
    openssl req -x509 -nodes -newkey rsa:2048 \
      -keyout "$KEY_PATH" \
      -out "$CERT_PATH" \
      -days 1 \
      -subj "/CN=$SITE_HOST" \
      -addext "subjectAltName=DNS:$SITE_HOST"
fi

# Запускаем основную команду (nginx)
exec "$@"
