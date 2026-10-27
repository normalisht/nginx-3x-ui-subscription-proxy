# nginx-3x-ui-subscription-proxy

[English version](README.md)

Объединяет подписки нескольких серверов [3x-UI](https://github.com/MHSanaei/3x-ui) (и, по желанию, сторонних VPN-сервисов) в одну ссылку. Удобно, когда серверы стоят в разных странах, а клиенту нужно добавить одну подписку.

Это один Go-сервис в distroless-образе около 10 МБ: опрашивает источники параллельно, отдаёт `routing.json`, а в самостоятельном режиме сам выпускает и продлевает сертификат Let's Encrypt. Ни nginx, ни certbot, ни cron.

## Важно

1. У клиента должен быть одинаковый **subscription ID** на всех серверах 3x-UI.
2. На всех серверах 3x-UI должно быть включено шифрование подписки.

## Запуск

```bash
git clone https://github.com/normalisht/nginx-3x-ui-subscription-proxy.git
cd nginx-3x-ui-subscription-proxy
make env    # .env из .env.example — заполнить
make up     # сборка, запуск, ожидание healthcheck
```

Ссылка подписки — `https://<SITE_HOST>/<SUB>/<subscription_ID>`, например `https://sub.example.com/sub/abc123`.

## Настройки (`.env`)

| Переменная | Что это |
|---|---|
| `SERVERS` | адреса подписок 3x-UI через пробел; к каждому дописывается id клиента: `https://s1.com/sub/` → `https://s1.com/sub/<id>` |
| `EXTERNAL_SUBSCRIPTIONS` | необязательно: готовые ссылки сторонних подписок через пробел, запрашиваются как есть |
| `EXTERNAL_SUBSCRIPTION_HEADERS` | необязательно: заголовки для запросов к `EXTERNAL_SUBSCRIPTIONS` — имитация обычного VPN-клиента. Формат `Key: Value`, несколько — через `;;` или перенос строки, например `User-Agent: Happ/2.4.1 (Android 13; SDK 33);;Hwid: 1234567890abcdef` |
| `EXTERNAL_SUBSCRIPTIONS_JSON` | `1` — сторонние подписки отдают JSON-конфиги, они сливаются в один (массивы склеиваются, объекты объединяются, к повторяющимся `remarks` дописывается номер). `SERVERS` в этом режиме не используются |
| `SUB` | статическая часть пути подписки, по умолчанию `sub` |
| `SITE_HOST` | домен сервиса: на него выпускается сертификат и указывает ссылка на `routing.json` |
| `ROUTING_ENABLED` | `1` — добавлять в начало подписки ссылки на `https://<SITE_HOST>/routing/routing.json` (правила маршрутизации для Happ и аналогов) |
| `EMAIL` | необязательно: почта аккаунта Let's Encrypt (самостоятельный режим) |
| `COMPOSE_FILE` | `compose.edge.yaml` — режим за общим reverse-proxy (ниже) |

## Режимы

**Самостоятельный** (`compose.yaml`, по умолчанию): сервис слушает 80 и 443 и при первом HTTPS-запросе получает сертификат Let's Encrypt для `SITE_HOST` (проверка TLS-ALPN-01 или HTTP-01). Сертификаты хранятся в томе `certs`. С 80 — редирект на HTTPS.

**За общим reverse-proxy** (`compose.edge.yaml`): HTTP на `subproxy:8080` во внешней Docker-сети `proxy`, портов наружу нет. TLS держит общий прокси — [edge-proxy](https://github.com/normalisht/edge-proxy). В `.env` — `COMPOSE_FILE=compose.edge.yaml`.

## Адреса

| Путь | Ответ |
|---|---|
| `/<SUB>/<id>` | объединённая подписка в base64; `502`, если ни один источник ничего не отдал |
| `/routing/routing.json` | правила маршрутизации из `routing.json` (смонтирован — правка и `make restart`) |
| `/healthz` | `ok` |

Источники опрашиваются параллельно, у каждого — 10 секунд на ответ; недоступный пропускается, в лог пишется только его хост (id подписок в лог не попадают). Проверка TLS-сертификатов источников выключена намеренно: у панелей 3x-UI часто самоподписанные сертификаты.

## Команды

`make help` — список: `up`, `down`, `restart` (применить правки `.env` и `routing.json`), `update` (`git pull` и перезапуск), `status`, `logs`, `check` (gofmt, vet, тесты).

## Лицензия

MIT, см. `LICENSE`.
