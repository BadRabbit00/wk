# Инфраструктура: ресурсы и окружения

| id | назначение | где | отличия от prod | status |
|---|---|---|---|---|
| ENV-dev | разработка | ноутбук, `nix develop` + `process-compose` | без TLS, EXT-payments не подключена | implemented |
| ENV-prod | боевое | один VPS на NixOS | — | implemented |

### RES-pg-main — Основная БД
status: implemented

- kind: PostgreSQL 16 + pgvector
- where: ENV-prod — тот же VPS; ENV-dev — локально
- used by: SVC-orders (схема `orders`), SVC-recs (схема `recs`); каждый видит только свою схему
- access: CFG-DATABASE_URL, CFG-RECS_DATABASE_URL — разные пользователи БД
- backup: pg_dump раз в сутки на внешний диск, проверка восстановления раз в месяц
- managed by: nix-модуль `infra/nixos/postgres.nix`

### RES-nats — Брокер событий
status: implemented

- kind: NATS JetStream
- where: ENV-prod — тот же VPS
- used by: SVC-orders (публикует), SVC-recs (слушает)
- access: CFG-NATS_URL
- backup: не нужен — события вторичны, источник правды в RES-pg-main
- managed by: nix-модуль `infra/nixos/nats.nix`
