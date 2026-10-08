# Наблюдаемость

- logs: JSON в stdout → journald; обязательные поля `ts`, `level`, `svc`, `msg`
- must not log: SEC-pii, SEC-secrets

| id | вид | имя / путь | компонент | порог | что делать | status |
|---|---|---|---|---|---|---|
| OBS-orders.health | health | `GET /healthz` | SVC-orders | — | systemd перезапускает | implemented |
| OBS-orders.latency | metric | `http_request_duration_seconds{route="/v1/orders"}` | SVC-orders | p95 > 300ms 5m | смотреть RES-pg-main | implemented |
