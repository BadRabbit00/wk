# Наблюдаемость

## Логи

- format: <JSON, одна строка = одно событие; обязательные поля: `ts`, `level`, `svc`, `trace_id`, `msg`>
- where: <stdout → <Loki / journald>>
- must not log: SEC-pii, SEC-secrets

## Метрики, алерты, healthcheck

| id | вид | имя / путь | компонент | порог | что делать | status |
|---|---|---|---|---|---|---|
| OBS-<имя> | <metric / alert / health / dashboard> | `<orders_created_total / GET /healthz>` | SVC-<…> | <p95 > 300ms 5m> | <runbook / ссылка> | <…> |
