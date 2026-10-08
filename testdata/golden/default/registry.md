# Реестр ID

> Сгенерировано `docs/_meta/check.py`. Руками не править — перезапусти скрипт.
> Колонки: где определён, где в коде (маркер `doc:`), кто ссылается.

Всего ID: **76** · accepted: 4 · draft: 1 · implemented: 62 · open: 3 · planned: 4 · —: 2

## Система и домен

| id | название | status | owner | определён | в коде | используют |
|---|---|---|---|---|---|---|
| ACT-customer | покупатель (через фронтенд) | implemented |  | `system/overview.md:15` |  | FLOW-checkout, `system/overview.md`, ROLE-customer |
| ACT-operator | оператор магазина | implemented |  | `system/overview.md:16` |  |  |
| EXT-payments | Платёжный шлюз | planned |  | `system/overview.md:20` |  | SVC-orders, `components/orders/component.md`, CFG-PAYMENTS_API_KEY, ENV-dev, Q-orders.currency, STATE-order +1 |
| TERM-order | заказ |  |  | `system/glossary.md:5` |  |  |
| TERM-sku | SKU |  |  | `system/glossary.md:6` |  | DTO-order-item, TBL-order-items, DTO-recs-response |
| ENT-order | Заказ | implemented |  | `system/domain.md:5` |  | TBL-orders, TBL-order-items |
| STATE-order | Статусы заказа | implemented |  | `system/domain.md:17` |  | DTO-order, TBL-orders, JOB-orders.expire-unpaid, ENT-order, FLOW-checkout, EXT-payments |
| INV-order-min-total | Минимальная сумма заказа | implemented |  | `system/domain.md:46` |  | MOD-orders.pricing, API-orders.create, TBL-orders, CFG-ORDERS_MIN_TOTAL, ENT-order, ERR-orders.min-total +1 |
| INV-order-not-empty | В заказе есть хотя бы одна позиция | implemented |  | `system/domain.md:39` |  | API-orders.create, ENT-order, ERR-orders.empty |
| FLOW-checkout | Оформление заказа | implemented |  | `system/flows.md:3` |  | INDEX.md, API-orders.create |
| NFR-orders.latency | p95 < 300 ms при 50 rps | implemented |  | `system/nfr.md:5` |  | API-orders.create, ADR-0001 |
| NFR-recs.latency | p95 < 150 ms | planned |  | `system/nfr.md:6` |  | API-recs.for-user |

## Безопасность и ошибки

| id | название | status | owner | определён | в коде | используют |
|---|---|---|---|---|---|---|
| ROLE-customer | покупатель | implemented |  | `system/security.md:12` |  | API-orders.create, API-orders.get, API-recs.for-user, PERM-orders.create |
| PERM-orders.create | создавать свои заказы | implemented |  | `system/security.md:16` |  | API-orders.create |
| SEC-authn | Как пользователи входят | implemented |  | `system/security.md:3` |  | CFG-JWT_PUBLIC_KEY |
| SEC-pii | персональные данные | implemented |  | `system/security.md:20` |  | DTO-order-create, TBL-orders, `infra/observability.md` |
| SEC-secrets | секреты | implemented |  | `system/security.md:21` |  | `infra/observability.md` |
| ERR-orders.empty | INV-order-not-empty | implemented |  | `system/errors.md:12` |  | API-orders.create, INV-order-not-empty, `system/errors.md` |
| ERR-orders.min-total | INV-order-min-total | implemented |  | `system/errors.md:13` |  | CLS-orders.price-calculator, API-orders.create, INV-order-min-total |
| ERR-unauthorized | нет или невалидный JWT | implemented |  | `system/errors.md:14` |  | API-orders.create, API-orders.get, API-recs.for-user |
| ERR-validation | тело запроса не соответствует DTO | implemented |  | `system/errors.md:11` |  | API-orders.create |

## Компоненты и код

| id | название | status | owner | определён | в коде | используют |
|---|---|---|---|---|---|---|
| SVC-orders | Сервис заказов | implemented |  | `components/orders/component.md:3` |  | INDEX.md, MOD-orders.http, MOD-orders.pricing, MOD-orders.store, CLS-orders.price-calculator, API-orders.create +29 |
| SVC-recs | Сервис рекомендаций | implemented |  | `components/recs/component.md:3` |  | INDEX.md, EVT-order.created, PRM-recs.explain, MOD-recs.ranker, API-recs.for-user, DTO-recs-response +18 |
| MOD-orders.http | HTTP-слой | implemented | SVC-orders | `components/orders/code.md:9` | `src/orders/internal/http/doc.go:1` | `components/orders/code.md`, `components/orders/component.md`, PERM-orders.create |
| MOD-orders.pricing | Расчёт суммы | implemented | SVC-orders | `components/orders/code.md:17` | `src/orders/internal/pricing/doc.go:1` | `components/orders/code.md`, MOD-orders.http, CLS-orders.price-calculator, `components/orders/component.md` |
| MOD-orders.store | Работа с БД | implemented | SVC-orders | `components/orders/code.md:25` | `src/orders/internal/store/doc.go:1` | `components/orders/code.md`, MOD-orders.http, `components/orders/component.md` |
| MOD-recs.ranker | Подбор товаров | implemented | SVC-recs | `components/recs/code.md:3` | `src/recs/recs/ranker.py:1` | `components/recs/component.md` |
| CLS-orders.price-calculator | Считает сумму заказа | implemented | SVC-orders | `components/orders/code.md:31` | `src/orders/internal/pricing/total.go:3` | MOD-orders.pricing, DTO-order, INV-order-min-total, FLOW-checkout |

## Контракты

| id | название | status | owner | определён | в коде | используют |
|---|---|---|---|---|---|---|
| API-orders.create | Создать заказ | implemented | SVC-orders | `components/orders/contracts.md:3` | `src/orders/internal/http/orders.go:22` | INDEX.md, `components/orders/component.md`, TBL-orders, TBL-order-items, INV-order-not-empty, FLOW-checkout +2 |
| API-orders.get | Получить заказ | implemented | SVC-orders | `components/orders/contracts.md:17` | `src/orders/internal/http/orders.go:28` | INDEX.md, `components/orders/component.md`, TBL-orders |
| API-recs.for-user | Рекомендации для покупателя | implemented | SVC-recs | `components/recs/contracts.md:3` | `src/recs/recs/app.py:14` | INDEX.md, `components/recs/component.md`, TBL-recs.user-vectors, NFR-recs.latency, ACT-customer |
| EVT-order.created | Заказ создан | implemented | SVC-orders | `components/orders/contracts.md:25` | `src/orders/internal/http/orders.go:25`, `src/recs/recs/consumer.py:1` | INDEX.md, `components/orders/component.md`, API-orders.create, SVC-recs, `components/recs/component.md`, TBL-recs.user-vectors +4 |
| DTO-order | Заказ в ответе API | implemented | SVC-orders | `components/orders/contracts.md:52` | `src/orders/internal/http/orders.go:16` | API-orders.create, API-orders.get, Q-orders.currency |
| DTO-order-create | Тело запроса на создание заказа | implemented | SVC-orders | `components/orders/contracts.md:35` | `src/orders/internal/http/orders.go:3` | API-orders.create, INV-order-not-empty, FLOW-checkout |
| DTO-order-created | Payload события | implemented | SVC-orders | `components/orders/contracts.md:63` | `src/orders/internal/http/orders.go:19` | EVT-order.created, TBL-recs.user-vectors |
| DTO-order-item | Позиция заказа | implemented | SVC-orders | `components/orders/contracts.md:43` | `src/orders/internal/http/orders.go:9` | DTO-order-create, DTO-order |
| DTO-recs-response | Ответ с рекомендациями | implemented | SVC-recs | `components/recs/contracts.md:13` | `src/recs/recs/app.py:9` | API-recs.for-user, MDL-embed-e5 |
| PRM-recs.explain | Объяснение, почему товар рекомендован | draft | SVC-recs | `components/recs/ai.md:3` |  | INDEX.md, Q-recs.llm |

## Данные

| id | название | status | owner | определён | в коде | используют |
|---|---|---|---|---|---|---|
| TBL-order-items | Позиции заказа | implemented | SVC-orders | `components/orders/data.md:32` | `src/orders/migrations/0001_init.sql:4` | MOD-orders.store, `components/orders/component.md`, API-orders.create, `components/orders/data.md`, ENT-order, FLOW-checkout +1 |
| TBL-orders | Заказы | implemented | SVC-orders | `components/orders/data.md:8` | `src/orders/migrations/0001_init.sql:1` | MOD-orders.store, `components/orders/component.md`, API-orders.create, EVT-order.created, `components/orders/data.md`, TBL-order-items +4 |
| TBL-recs.user-vectors | Векторы покупателей | implemented | SVC-recs | `components/recs/data.md:3` | `src/recs/migrations/0001_init.sql:1` | `components/recs/component.md`, FLOW-checkout |

## Рантайм

| id | название | status | owner | определён | в коде | используют |
|---|---|---|---|---|---|---|
| JOB-orders.expire-unpaid | Отмена неоплаченных заказов | planned | SVC-orders | `components/orders/runtime.md:3` |  | INDEX.md, SVC-orders, `components/orders/component.md`, TBL-orders, STATE-order, EXT-payments |
| CFG-DATABASE_URL | подключение к RES-pg-main | implemented | SVC-orders | `components/orders/runtime.md:15` | `src/orders/cmd/orders/main.go:6` | `components/orders/component.md`, RES-pg-main |
| CFG-JWT_PUBLIC_KEY | проверка токенов SEC-authn | implemented | SVC-orders | `components/orders/runtime.md:18` | `src/orders/cmd/orders/main.go:9` | `components/orders/component.md`, SEC-authn |
| CFG-NATS_URL | подключение к RES-nats | implemented | SVC-orders | `components/orders/runtime.md:16` | `src/orders/cmd/orders/main.go:7` | `components/orders/component.md`, RES-nats |
| CFG-ORDERS_MIN_TOTAL | INV-order-min-total | implemented | SVC-orders | `components/orders/runtime.md:17` | `src/orders/cmd/orders/main.go:8` | `components/orders/component.md`, TBL-orders, INV-order-min-total |
| CFG-PAYMENTS_API_KEY | доступ к EXT-payments | planned | SVC-orders | `components/orders/runtime.md:19` |  | `components/orders/component.md`, EXT-payments |
| CFG-RECS_DATABASE_URL | подключение к RES-pg-main | implemented | SVC-recs | `components/recs/runtime.md:5` | `src/recs/recs/app.py:5` | `components/recs/component.md`, RES-pg-main |
| CFG-RECS_MODEL_PATH | где лежат веса MDL-embed-e5 | implemented | SVC-recs | `components/recs/runtime.md:6` | `src/recs/recs/app.py:6` | `components/recs/component.md`, MDL-embed-e5 |

## Стек

| id | название | status | owner | определён | в коде | используют |
|---|---|---|---|---|---|---|
| LANG-go1.23 | Go 1.23 | implemented |  | `stack/languages.md:5` |  | INDEX.md, SVC-orders, ADR-0001 |
| LANG-python3.12 | Python 3.12 | implemented |  | `stack/languages.md:6` |  | INDEX.md, SVC-recs, ADR-0002 |
| TOOL-nix | Nix flakes | implemented |  | `stack/languages.md:10` |  | INDEX.md, SVC-orders, SVC-recs |
| TOOL-uv | uv | implemented |  | `stack/languages.md:11` |  | SVC-recs |
| DEP-go-chi | github.com/go-chi/chi/v5 | implemented |  | `stack/dependencies.md:5` |  | INDEX.md, SVC-orders, ADR-0001 |
| DEP-go-decimal | github.com/shopspring/decimal | implemented |  | `stack/dependencies.md:8` |  | ADR-0001 |
| DEP-go-nats | github.com/nats-io/nats.go | implemented |  | `stack/dependencies.md:7` |  | INDEX.md, SVC-orders |
| DEP-go-pgx | github.com/jackc/pgx/v5 | implemented |  | `stack/dependencies.md:6` |  | INDEX.md, SVC-orders, ADR-0001 |
| DEP-py-fastapi | fastapi | implemented |  | `stack/dependencies.md:9` |  | INDEX.md, SVC-recs, ADR-0002 |
| DEP-py-nats | nats-py | implemented |  | `stack/dependencies.md:12` |  | SVC-recs |
| DEP-py-psycopg | psycopg[binary] | implemented |  | `stack/dependencies.md:11` |  | SVC-recs |
| DEP-py-sentence-transformers | sentence-transformers | implemented |  | `stack/dependencies.md:10` |  | INDEX.md, SVC-recs, ADR-0002, MDL-embed-e5 |
| MDL-embed-e5 | Эмбеддинги товаров | implemented |  | `stack/models.md:3` |  | INDEX.md, SVC-recs, `components/recs/component.md`, TBL-recs.user-vectors, CFG-RECS_MODEL_PATH, ADR-0002 +2 |

## Инфраструктура

| id | название | status | owner | определён | в коде | используют |
|---|---|---|---|---|---|---|
| RES-nats | Брокер событий | implemented |  | `infra/resources.md:18` |  | INDEX.md, `components/orders/component.md`, EVT-order.created, CFG-NATS_URL, DEP-go-nats, FLOW-checkout +1 |
| RES-pg-main | Основная БД | implemented |  | `infra/resources.md:8` |  | INDEX.md, `components/orders/component.md`, TBL-orders, TBL-order-items, CFG-DATABASE_URL, `components/recs/component.md` +4 |
| ENV-dev | разработка | implemented |  | `infra/resources.md:5` |  | RES-pg-main |
| ENV-prod | боевое | implemented |  | `infra/resources.md:6` |  | `components/orders/component.md`, `components/recs/component.md`, PIPE-main, RES-pg-main, RES-nats |
| PIPE-main | Проверка и деплой из main | implemented |  | `infra/delivery.md:3` |  |  |
| OBS-orders.health | GET /healthz | implemented |  | `infra/observability.md:8` |  | `components/orders/component.md` |
| OBS-orders.latency | http_request_duration_seconds{route="/v1/orders"} | implemented |  | `infra/observability.md:9` |  | NFR-orders.latency |

## Решения и незакрытое

| id | название | status | owner | определён | в коде | используют |
|---|---|---|---|---|---|---|
| ADR-0001 | Go для сервиса заказов | accepted |  | `decisions/ADR-0001-go-for-orders.md:1` |  | `components/orders/component.md`, LANG-go1.23 |
| ADR-0002 | Python для рекомендаций | accepted |  | `decisions/ADR-0002-python-for-recs.md:1` |  | `components/recs/component.md`, DEP-py-fastapi, LANG-python3.12 |
| Q-orders.currency | только тенге или несколько валют? От этого зависит DTO-order и интеграция с EXT-payments | open |  | `open-questions.md:5` |  | INDEX.md, STATE-order |
| Q-recs.llm | какую LLM использовать для PRM-recs.explain: локальную или EXT-провайдера? | open |  | `open-questions.md:6` |  | PRM-recs.explain |
| RISK-ops.no-admin-api | у оператора нет API | accepted |  | `risks.md:6` |  | ACT-operator |
| RISK-orders.no-outbox | событие публикуется после коммита без outbox | open |  | `risks.md:5` |  | `components/orders/component.md`, FLOW-checkout |
| RISK-recs.single-gpu | MDL-embed-e5 крутится на одном GPU | accepted |  | `risks.md:7` |  | `components/recs/component.md` |
