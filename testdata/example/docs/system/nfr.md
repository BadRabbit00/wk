# Нефункциональные требования

| id | категория | требование | как меряем | к чему относится | status |
|---|---|---|---|---|---|
| NFR-orders.latency | скорость | p95 < 300 ms при 50 rps | OBS-orders.latency | API-orders.create | implemented |
| NFR-recs.latency | скорость | p95 < 150 ms | — | API-recs.for-user | planned |
