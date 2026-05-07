# Используем OpenResty на базе Alpine
FROM openresty/openresty:alpine-fat

# Прокидываем переменные
ENV SITE_HOST=localhost
ENV SUB=sub

# Выставляем порты
EXPOSE 443/tcp 80/tcp

# Копируем конфигурацию Nginx
RUN rm /usr/local/openresty/nginx/conf/nginx.conf
COPY nginx.conf.esh /usr/local/openresty/nginx/conf/

# Устанавливаем esh и certbot
RUN apk upgrade  \
    && apk add --no-cache esh certbot openssl

# Копируем конфигурацию
COPY crontab.txt /etc/crontabs/root
COPY update-certs.sh entrypoint.sh /usr/local/bin/
COPY routing.json /var/www/

# Устанавливаем права на файлы
RUN chmod -R 755 /usr/local/openresty/nginx/conf/ \
    && chmod +x /usr/local/bin/update-certs.sh  \
    && chmod +x /usr/local/bin/entrypoint.sh

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

# Запускаем nginx со своей конфигурацией
CMD ["/bin/sh", "-c", "esh -o /usr/local/openresty/nginx/conf/nginx.conf /usr/local/openresty/nginx/conf/nginx.conf.esh && exec nginx -g 'daemon off;'"]
