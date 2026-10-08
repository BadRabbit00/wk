# Безопасность

### SEC-authn — Как пользователи входят
status: implemented

- method: JWT от внешнего сервиса авторизации; мы только проверяем подпись
- token: заголовок `Authorization: Bearer`, публичный ключ в CFG-JWT_PUBLIC_KEY
- service-to-service: сервисы общаются только через RES-nats, HTTP между ними нет

| id | роль | кто это | status |
|---|---|---|---|
| ROLE-customer | покупатель | ACT-customer | implemented |

| id | право | роли | где проверяется | status |
|---|---|---|---|---|
| PERM-orders.create | создавать свои заказы | ROLE-customer | MOD-orders.http (middleware) | implemented |

| id | класс | примеры | где лежит | требования | status |
|---|---|---|---|---|---|
| SEC-pii | персональные данные | адрес доставки | TBL-orders#delivery_address | не логировать, удалять через 3 года | implemented |
| SEC-secrets | секреты | ключи БД, платёжки, JWT | sops-файл в `infra/secrets/` | никогда не логировать | implemented |
