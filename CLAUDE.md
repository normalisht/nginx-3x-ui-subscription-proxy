# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Что это за проект

Nginx-прокси, объединяющий подписки (subscription configs) с нескольких серверов [3x-UI](https://github.com/MHSanaei/3x-ui) в одну точку входа. Клиент обращается по одному URL, а бэкенд агрегирует конфиги со всех перечисленных 3x-UI серверов, добавляет к ним routing-правила и отдаёт результат в base64.

## Архитектура

Два контейнера, поднимаемые через `docker-compose.yml`:

- **nginx** (`Dockerfile`, база `openresty/openresty:alpine-fat`) — слушает 80/443, терминирует SSL, обслуживает `/routing/` (статика из `routing.json`) и `/.well-known/acme-challenge/` (Certbot), а запросы `/<SUB>/<subscription_ID>` проксирует на `python:8080` (upstream `python_backend`). Конфиг шаблонизируется через `esh`: `nginx.conf.esh` → `nginx.conf` при старте контейнера (см. `CMD` в `Dockerfile`), плейсхолдеры вида `<%= $SITE_HOST %>` подставляются из переменных окружения.
- **python** (`Dockerfile_python`, `config_fetcher.py`) — простой HTTP-сервер на стандартной библиотеке (без фреймворка), слушает `PORT` (по умолчанию 8080). На GET `/<SUB>/<id>` обходит все `SERVERS`, для каждого скачивает `<server>/<id>`, декодирует base64, а также обходит все `EXTERNAL_SUBSCRIPTIONS` — готовые сторонние ссылки подписок (например, на другие VPN-сервисы), которые запрашиваются как есть, без дописывания `<id>`. Результаты конкатенируются с двумя служебными строками роутинга (`://autorouting/onadd/...` и `://routing/onadd/...`, указывающими на `https://<SITE_HOST>/routing/routing.json`), затем всё вместе кодируется обратно в base64 и отдаётся как `text/plain`.

Поток запроса: `client → nginx (443, SSL) → python_backend:8080 → N × 3x-UI servers`; отдельно nginx отдаёт `routing.json` по `/routing/`.

SSL-сертификаты (Let's Encrypt) выпускаются/обновляются через Certbot:
- `entrypoint.sh` — точка входа nginx-контейнера: запускает `crond`, при отсутствии сертификата для `$SITE_HOST` генерирует временный self-signed (на 1 день), затем передаёт управление основной команде (рендер `esh` + `nginx`).
- `update-certs.sh` — выпускает сертификат через `certbot certonly --webroot` (если сертификата ещё нет) либо продлевает через `certbot renew`; после успешной операции делает `nginx -s reload`.
- `crontab.txt` — cron-задача, ежедневно в 12:00 запускающая `update-certs.sh`.

## Конфигурация окружения

Переменные задаются через `.env` (см. `.env.example`) и пробрасываются в оба контейнера через `env_file` в `docker-compose.yml`:

| Переменная   | Назначение                                                                 |
|--------------|-----------------------------------------------------------------------------|
| `SITE_HOST`  | Домен nginx-сервера, используется в SSL-сертификате и в ссылке на `routing.json` |
| `SERVERS`    | Список URL 3x-UI серверов через пробел (например `https://s1.com/sub/ https://s2.com/sub/`) |
| `SUB`        | Статическая часть пути подписки (например `sub`)                          |
| `PORT`       | Порт Python-бэкенда (по умолчанию 8080)                                   |
| `EMAIL`      | Email для Certbot (используется в `update-certs.sh`)                      |
| `EXTERNAL_SUBSCRIPTIONS` | Необязательный список полных URL сторонних subscription-ссылок через пробел (например, готовая ссылка от стороннего VPN-сервиса). В отличие от `SERVERS`, к этим URL ничего не дописывается — они запрашиваются как есть и подмешиваются в общий конфиг наравне с остальными |

Важно: у клиента должен быть одинаковый subscription ID на всех 3x-UI серверах, и на всех серверах должно быть включено шифрование подписки.

## Команды разработки

Запуск всего стека:
```bash
docker compose up -d --build
```

Логи отдельного сервиса:
```bash
docker compose logs -f nginx
docker compose logs -f python
```

Локальный запуск python-бэкенда без Docker (для отладки `config_fetcher.py`):
```bash
python3 -m venv venv && source venv/bin/activate
pip install -r requirements.txt
SITE_HOST=localhost SUB=sub SERVERS="https://server1.com/sub/" PORT=8080 python config_fetcher.py
```

Ручная проверка эндпоинта:
```bash
curl http://localhost:8080/sub/<subscription_ID>
```

Тестов и линтеров в проекте нет.

## Особенности при внесении изменений

- `nginx.conf.esh` — не обычный nginx.conf, а шаблон `esh` (embedded shell), плейсхолдеры `<%= $VAR %>` подставляются на старте контейнера через `entrypoint.sh`/`Dockerfile` CMD, а не при сборке образа.
- `routing.json` — конфигурация роутинга клиентского прокси-приложения (Happ/аналоги), раздаётся статикой через `/routing/` и подмешивается python-бэкендом в каждый ответ подписки как отдельная запись. Это не относится к nginx routing/location.
- `requests.get(..., verify=False)` в `config_fetcher.py` — намеренное отключение проверки TLS при обращении к 3x-UI серверам (например, если у них self-signed сертификаты); учитывайте это при рефакторинге.
