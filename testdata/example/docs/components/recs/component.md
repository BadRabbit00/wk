# recs

### SVC-recs — Сервис рекомендаций
status: implemented | kind: http-service | lang: LANG-python3.12

- purpose: отдаёт покупателю список товаров, похожих на то, что он уже покупал
- not responsible for: каталог товаров (берём SKU из событий), ранжирование по наличию на складе
- path: `src/recs/`
- manifest: `src/recs/pyproject.toml`, lock: `src/recs/uv.lock`
- build: TOOL-uv внутри TOOL-nix; `nix build .#recs`
- run locally: `nix develop -c uv run --project src/recs uvicorn recs.app:app --port 8081`
- test: `nix develop -c uv run --project src/recs pytest`
- entrypoints: `src/recs/recs/app.py` — HTTP; `src/recs/recs/consumer.py` — подписчик на EVT-order.created
- framework: DEP-py-fastapi
- deps: DEP-py-sentence-transformers, DEP-py-psycopg, DEP-py-nats
- models: MDL-embed-e5

## Что компонент даёт другим

- API: API-recs.for-user

## Что компонент использует

| что | зачем | что будет, если недоступно |
|---|---|---|
| EVT-order.created | обновить вектор покупателя | рекомендации устаревают |
| MDL-embed-e5 | эмбеддинги SKU | отдаём популярные товары (RISK-recs.single-gpu) |

## Данные

- owns: TBL-recs.user-vectors
- store: RES-pg-main, схема `recs`, расширение pgvector
- migrations: `src/recs/migrations/*.sql`, применяются при старте
- reads foreign data: нет — заказы узнаём только из EVT-order.created

## Устройство

- modules: MOD-recs.ranker
- config: CFG-RECS_DATABASE_URL, CFG-RECS_MODEL_PATH

## Развёртывание

- runs on: ENV-prod, хост с GPU
- scaling: одна реплика
- health: `GET /healthz`

## Решения

- ADR-0002 — Python для рекомендаций
