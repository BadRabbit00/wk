# recs: настройки

| id | owner | вид | тип | по умолчанию | обязательна | секрет | на что влияет | status |
|---|---|---|---|---|---|---|---|---|
| CFG-RECS_DATABASE_URL | SVC-recs | env | string | — | да | да | подключение к RES-pg-main | implemented |
| CFG-RECS_MODEL_PATH | SVC-recs | env | string | `/var/lib/recs/e5` | нет | нет | где лежат веса MDL-embed-e5 | implemented |
