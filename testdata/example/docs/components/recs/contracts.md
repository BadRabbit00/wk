# recs: контракты

### API-recs.for-user — Рекомендации для покупателя
status: implemented | owner: SVC-recs | kind: http

- endpoint: `GET /v1/recs?limit=10`
- auth: ROLE-customer (покупатель из JWT)
- response: 200 → DTO-recs-response
- errors: ERR-unauthorized
- limits: `limit` 1..50 (NFR-recs.latency)
- consumers: фронтенд магазина

### DTO-recs-response — Ответ с рекомендациями
status: implemented | owner: SVC-recs

| поле | тип | обяз. | ограничения | смысл |
|---|---|---|---|---|
| `skus` | string[] | да | ≤ limit | TERM-sku по убыванию релевантности |
| `fallback` | bool | да | | `true`, если отдали популярное вместо персонального |
