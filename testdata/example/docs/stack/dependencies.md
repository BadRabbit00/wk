# Внешние зависимости всех компонентов

| id | пакет | версия | компоненты | зачем | почему не другое | status |
|---|---|---|---|---|---|---|
| DEP-go-chi | `github.com/go-chi/chi/v5` | `v5.1` | SVC-orders | HTTP-роутер | совместим с `net/http`, без своего контекста как у gin | implemented |
| DEP-go-pgx | `github.com/jackc/pgx/v5` | `v5.7` | SVC-orders | драйвер PostgreSQL | быстрее `database/sql`, нативные типы PG | implemented |
| DEP-go-nats | `github.com/nats-io/nats.go` | `v1.37` | SVC-orders | публикация в RES-nats | официальный клиент | implemented |
| DEP-go-decimal | `github.com/shopspring/decimal` | `v1.4` | SVC-orders | деньги без float | стандарт де-факто | implemented |
| DEP-py-fastapi | `fastapi` | `^0.115` | SVC-recs | HTTP | ADR-0002 | implemented |
| DEP-py-sentence-transformers | `sentence-transformers` | `^3.0` | SVC-recs | запуск MDL-embed-e5 | проще, чем голый transformers | implemented |
| DEP-py-psycopg | `psycopg[binary]` | `^3.2` | SVC-recs | драйвер PostgreSQL + pgvector | psycopg2 устарел | implemented |
| DEP-py-nats | `nats-py` | `^2.9` | SVC-recs | подписка на EVT-order.created | официальный клиент | implemented |
