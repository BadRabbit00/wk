# recs: устройство кода

### MOD-recs.ranker — Подбор товаров
status: implemented | owner: SVC-recs

- path: `src/recs/recs/ranker.py`
- responsibility: ближайшие SKU к вектору покупателя, fallback на популярные
- must not: вызывать модель синхронно на каждый запрос — эмбеддинги SKU считаются заранее
