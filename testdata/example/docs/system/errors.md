# Ошибки

Формат ответа одинаковый для всех API:

```json
{ "error": { "code": "ERR-orders.empty", "message": "order has no items", "details": {} } }
```

| id | http | когда возникает | можно повторить? | что делать клиенту | status |
|---|---|---|---|---|---|
| ERR-validation | 422 | тело запроса не соответствует DTO | нет | исправить запрос, поля в `details` | implemented |
| ERR-orders.empty | 422 | INV-order-not-empty | нет | добавить позиции | implemented |
| ERR-orders.min-total | 422 | INV-order-min-total | нет | показать минимальную сумму из `details.min` | implemented |
| ERR-unauthorized | 401 | нет или невалидный JWT | после перелогина | обновить токен | implemented |
