#!/bin/sh
set -e

EMAIL="$EMAIL"
WEBROOT_PATH="/var/www/certbot"
CERT_DIR="/etc/letsencrypt/live/$SITE_HOST"
CERT_PATH="$CERT_DIR/fullchain.pem"
KEY_PATH="$CERT_DIR/privkey.pem"

if [ ! -f "$CERT_PATH" ] || [ ! -f "$KEY_PATH" ]; then
    echo "Сертификаты для $SITE_HOST не найдены или неполны. Получаем новые..."
    certbot certonly --webroot \
        -w $WEBROOT_PATH \
        -d $SITE_HOST \
        --email $EMAIL \
        --agree-tos \
        --non-interactive \
        --deploy-hook "nginx -s reload"
else
    echo "Сертификаты для $SITE_HOST уже существуют."
    certbot renew --webroot -w $WEBROOT_PATH --non-interactive --deploy-hook "nginx -s reload"
fi
