# Нейросети и ML-модели

### MDL-embed-e5 — Эмбеддинги товаров
status: implemented

- task: эмбеддинги текста (название + категория SKU)
- source: HF `intfloat/multilingual-e5-small`, revision закреплён в `flake.lock`
- license: MIT
- size / format: 118M параметров, safetensors, ~470 MB
- runtime: DEP-py-sentence-transformers, GPU не обязателен
- weights stored: nix store → путь в CFG-RECS_MODEL_PATH
- used by: SVC-recs
- fallback: популярные товары (`fallback: true` в DTO-recs-response)
