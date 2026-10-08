# orders: фоновые задачи и настройки

### JOB-orders.expire-unpaid — Отмена неоплаченных заказов
status: planned | owner: SVC-orders

- trigger: каждые 5m внутри процесса SVC-orders
- does: заказы в STATE-order#new старше 30m переводит в `cancelled`
- touches: пишет TBL-orders
- idempotent: да — повторный запуск ничего не меняет
- concurrency: только одна реплика сервиса (см. SVC-orders, scaling)
- on failure: лог уровня error; алерта пока нет

| id | owner | вид | тип | по умолчанию | обязательна | секрет | на что влияет | status |
|---|---|---|---|---|---|---|---|---|
| CFG-DATABASE_URL | SVC-orders | env | string | — | да | да | подключение к RES-pg-main | implemented |
| CFG-NATS_URL | SVC-orders | env | string | `nats://localhost:4222` | нет | нет | подключение к RES-nats | implemented |
| CFG-ORDERS_MIN_TOTAL | SVC-orders | env | decimal(12,2) | `500.00` | нет | нет | INV-order-min-total | implemented |
| CFG-JWT_PUBLIC_KEY | SVC-orders | env | string | — | да | нет | проверка токенов SEC-authn | implemented |
| CFG-PAYMENTS_API_KEY | SVC-orders | secret | string | — | нет | да | доступ к EXT-payments | planned |
