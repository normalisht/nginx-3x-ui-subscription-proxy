# Команды прокси подписок. make help — список целей.
# Режим (самостоятельный или за edge-proxy) задаёт COMPOSE_FILE в .env — docker compose читает его сам.

.DEFAULT_GOAL := help
.PHONY: help env up down restart update status logs test check

help: ## Показать список команд
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

env: ## Создать .env из примера (если ещё нет) — затем вписать SERVERS, SUB, SITE_HOST
	@if [ -f .env ]; then echo ".env уже есть — не трогаю"; else cp .env.example .env && chmod 600 .env && echo "Создан .env — заполните его"; fi

up: ## Собрать, запустить и дождаться healthcheck
	docker compose up -d --build --wait --remove-orphans

down: ## Остановить
	docker compose down

# Пересоздание, а не docker compose restart: routing.json смонтирован файлом, и после git pull
# (новый файл — новый inode) просто перезапущенный контейнер видел бы старую версию.
restart: ## Применить правки .env и routing.json: пересоздать контейнер
	docker compose up -d --build --force-recreate --wait --remove-orphans

update: ## git pull и перезапуск
	git pull --ff-only
	@$(MAKE) --no-print-directory restart

status: ## Состояние контейнера
	docker compose ps

logs: ## Логи
	docker compose logs -f --tail=200

test: ## Тесты
	go test ./...

check: ## gofmt, go vet и тесты — перед коммитом
	@test -z "$$(gofmt -l .)" || { echo "gofmt: $$(gofmt -l .)"; exit 1; }
	go vet ./...
	go test ./...
