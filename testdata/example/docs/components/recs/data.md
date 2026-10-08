# recs: данные

### TBL-recs.user-vectors — Векторы покупателей
status: implemented | owner: SVC-recs | store: RES-pg-main

- purpose: средний эмбеддинг купленных товаров по каждому покупателю
- pk: `customer_id`
- retention: бессрочно
- pii: нет

| колонка | тип | null | default | ограничения | смысл |
|---|---|---|---|---|---|
| `customer_id` | uuid | нет | | pk | из DTO-order-created |
| `vec` | float64[] | нет | | размерность 384 (MDL-embed-e5) | pgvector `vector(384)` |
| `orders_seen` | int32 | нет | 0 | | для скользящего среднего |
| `last_order_id` | uuid | нет | | | защита от повторной обработки EVT-order.created |

- indexes: HNSW по `vec` (cosine)
- written by: подписчик на EVT-order.created
- read by: API-recs.for-user
