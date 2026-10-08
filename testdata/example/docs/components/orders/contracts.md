# orders: контракты

### API-orders.create — Создать заказ
status: implemented | owner: SVC-orders | kind: http

- endpoint: `POST /v1/orders`
- auth: ROLE-customer с PERM-orders.create
- request: DTO-order-create
- response: 201 → DTO-order
- errors: ERR-validation, ERR-orders.empty, ERR-orders.min-total, ERR-unauthorized
- idempotency: заголовок `Idempotency-Key` обязателен; повтор с тем же ключом в течение 24h вернёт тот же заказ
- side effects: пишет TBL-orders, TBL-order-items; после коммита публикует EVT-order.created
- implements: FLOW-checkout (шаги 1–4), INV-order-not-empty, INV-order-min-total
- limits: тело ≤ 64 KB, таймаут 5s (NFR-orders.latency)
- consumers: фронтенд магазина

### API-orders.get — Получить заказ
status: implemented | owner: SVC-orders | kind: http

- endpoint: `GET /v1/orders/{id}`
- auth: ROLE-customer, только свой заказ (чужой → 404, а не 403, чтобы не раскрывать существование)
- response: 200 → DTO-order
- errors: ERR-unauthorized

### EVT-order.created — Заказ создан
status: implemented | owner: SVC-orders | kind: event

- channel: RES-nats, subject `orders.created.v1`
- payload: DTO-order-created
- when: после коммита транзакции в TBL-orders
- delivery: at-least-once; dedup key: `order_id`
- ordering: не гарантирован
- consumers: SVC-recs — обновляет вектор покупателя

### DTO-order-create — Тело запроса на создание заказа
status: implemented | owner: SVC-orders

| поле | тип | обяз. | ограничения | смысл |
|---|---|---|---|---|
| `items` | DTO-order-item[] | да | 1..50 | позиции |
| `delivery_address` | string | да | 5..300 символов | адрес, SEC-pii |

### DTO-order-item — Позиция заказа
status: implemented | owner: SVC-orders

| поле | тип | обяз. | ограничения | смысл |
|---|---|---|---|---|
| `sku` | string | да | TERM-sku | товар |
| `qty` | int32 | да | 1..99 | количество |
| `unit_price` | decimal(12,2) | да | > 0 | цена за штуку от фронтенда |

### DTO-order — Заказ в ответе API
status: implemented | owner: SVC-orders

| поле | тип | обяз. | ограничения | смысл |
|---|---|---|---|---|
| `id` | uuid | да | | |
| `status` | enum(STATE-order) | да | | |
| `items` | DTO-order-item[] | да | | |
| `total` | decimal(12,2) | да | | посчитано CLS-orders.price-calculator |
| `created_at` | timestamp | да | | |

### DTO-order-created — Payload события
status: implemented | owner: SVC-orders

| поле | тип | обяз. | ограничения | смысл |
|---|---|---|---|---|
| `order_id` | uuid | да | | |
| `customer_id` | uuid | да | | из JWT |
| `skus` | string[] | да | | без цен и адреса — PII в события не кладём |

- serialization: JSON, snake_case
