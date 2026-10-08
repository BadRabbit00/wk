# Сквозные сценарии

### FLOW-checkout — Оформление заказа
status: implemented

- trigger: ACT-customer вызывает API-orders.create
- result: заказ в STATE-order#new, рекомендации покупателя учитывают покупку

1. ACT-customer → API-orders.create с DTO-order-create.
2. SVC-orders считает сумму (CLS-orders.price-calculator), проверяет INV-order-min-total.
3. SVC-orders в одной транзакции пишет TBL-orders и TBL-order-items.
4. После коммита SVC-orders публикует EVT-order.created в RES-nats.
5. SVC-recs получает событие и пересчитывает вектор покупателя в TBL-recs.user-vectors.

- failures:
  - шаг 4: NATS недоступен → заказ уже создан, событие теряется (RISK-orders.no-outbox)
  - шаг 5: ошибка в SVC-recs → JetStream передоставит событие, обработка идемпотентна
- consistency: рекомендации могут отставать от заказов на секунды; это допустимо
