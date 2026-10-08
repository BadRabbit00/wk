# orders

### SVC-orders — Сервис заказов
status: implemented | kind: http-service | lang: LANG-go1.23

- purpose: принимает и хранит заказы, считает их сумму, сообщает остальным о новых заказах
- not responsible for: каталог и цены товаров (приходят от фронтенда), оплата (EXT-payments, позже)
- path: `src/orders/`
- manifest: `src/orders/go.mod`, lock: `src/orders/go.sum`
- build: TOOL-nix; `nix build .#orders`
- run locally: `nix develop -c go run ./src/orders/cmd/orders` (порт 8080)
- test: `nix develop -c go test ./src/orders/...`
- entrypoints: `src/orders/cmd/orders/main.go` — HTTP-сервер и воркер JOB-orders.expire-unpaid
- framework: DEP-go-chi
- deps: DEP-go-pgx, DEP-go-nats

## Что компонент даёт другим

- API: API-orders.*
- events published: EVT-order.created

## Что компонент использует

| что | зачем | что будет, если недоступно |
|---|---|---|
| RES-nats | публикация EVT-order.created | заказ создаётся, событие теряется (RISK-orders.no-outbox) |
| EXT-payments | оплата (planned) | — |

## Данные

- owns: TBL-orders, TBL-order-items
- store: RES-pg-main, схема `orders`
- migrations: `src/orders/migrations/*.sql`, применяются при старте сервиса
- reads foreign data: нет

## Устройство

- modules: MOD-orders.http, MOD-orders.pricing, MOD-orders.store
- jobs: JOB-orders.expire-unpaid
- config: CFG-DATABASE_URL, CFG-NATS_URL, CFG-ORDERS_MIN_TOTAL, CFG-JWT_PUBLIC_KEY, CFG-PAYMENTS_API_KEY

## Развёртывание

- runs on: ENV-prod
- unit: nix-модуль `infra/nixos/orders.nix`, systemd, 256 MB RAM
- scaling: одна реплика (JOB-orders.expire-unpaid не умеет работать параллельно)
- health: OBS-orders.health

## Решения

- ADR-0001 — Go для сервиса заказов
