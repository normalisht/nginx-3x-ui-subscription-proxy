# Деплой

Как устроен CI/CD: что происходит при MR и вмерже, что завести один раз на VPS и в GitHub, как катить и откатывать.

## Пайплайн

```
PR в main ──► ci.yml: golangci-lint + make check (gofmt, vet, тесты)
                       │
merge в main           ▼
main ──► release.yml: build-push: docker build (Dockerfile) ──► ghcr.io/normalisht/nginx-3x-ui-subscription-proxy
                       │                                 теги: latest, <sha>
                       ▼
        release.yml: deploy (needs: build-push) — автоматом
                       │
                       ▼
        SSH на VPS: git pull → docker compose pull → up -d --wait
                       │
                       ▼
        healthcheck https://config.normanproject.ru/healthz
```

- **Образ собирается только в CI** — на сервере сборки нет, только `docker compose pull`.
- **Автодеплой** — джоба `deploy` стартует сама после успешной сборки при пуше в main: выкатывается ровно тот sha, который собран. Ручной запуск `release.yml` (Actions → «Релиз» → Run workflow) остался для отката/перевыката: в поле `image_tag` указывается sha старого образа или `latest`.
- **IMAGE_TAG = sha коммита** — деплой уводит ровно тот коммит, который собран; `latest` — синоним свежего.
- Режим (самостоятельный или за edge-proxy) на сервере задаёт `COMPOSE_FILE` в `.env` — деплой его не меняет.

## Разовая настройка VPS

1. Склонировать репозиторий в каталог деплоя (HTTPS с PAT с `contents:read` — что уже настроено для ручного деплоя):
   ```bash
   git clone https://github.com/normalisht/nginx-3x-ui-subscription-proxy.git ~/nginx-3x-ui-subscription-proxy
   ```
2. Заполнить `.env` (если ещё не): `make env`, вписать `SERVERS`, `SUB`, `SITE_HOST` (и `COMPOSE_FILE` для режима за edge).
3. Войти в GHCR (образ приватный вместе с репозиторием): создать PAT с `read:packages` и `contents:read`, затем:
   ```bash
   echo "<PAT>" | docker login ghcr.io -u normalisht --password-stdin
   ```
4. Последний раз руками: `make up` (после этого compose ссылается на GHCR-образ).

## Настройка GitHub

| Имя | Где | Что это |
|-----|-----|---------|
| `SSH_HOST` | Secrets | хост VPS |
| `SSH_USER` | Secrets | пользователь SSH |
| `SSH_KEY` | Secrets | приватный ключ (формат OpenSSH, целиком, с переводами строк) |
| `DEPLOY_DIR` | Variables | каталог деплоя на VPS, например `/root/nginx-3x-ui-subscription-proxy` |

Окружение `production` (Settings → Environments) создано; на бесплатном плане правила окружений недоступны — деплой автоматический.

## Откат

Образы каждого коммита остаются в GHCR. Два способа:

- **Через Actions** (рекомендуется): Actions → «Релиз» → Run workflow, в `image_tag` — sha нужного коммита (или `latest`). Джоба deploy выкатит указанный образ на VPS.
- **Руками на VPS**:

```bash
cd ~/nginx-3x-ui-subscription-proxy
IMAGE_TAG=<sha> docker compose up -d --wait
```

Или «на свежий, но прошлый»: `IMAGE_TAG` не трогать, `docker compose pull` после revert-коммита в main (автодеплой выкатит сам).

## Замечания

- `git pull --ff-only` в деплое обновляет compose-файлы и `routing.json`; если серверный клон потерял доступ (истёк PAT), деплой упадёт на этом шаге.
- Healthcheck в compose (`/subproxy healthcheck`) — внутри контейнера; шаг «Healthcheck» в workflow проверяет `/healthz` снаружи, через боевой домен.
- В режиме edge смена upstream не нужна: имя сервиса `subproxy` не менялось.
