# Нейросети и ML-модели

### MDL-<имя> — <Задача модели>
status: <…>

- task: <классификация / эмбеддинги / генерация текста / распознавание>
- source: <HF `<org/model>` @ revision `<commit>` / EXT-<провайдер> модель `<name>` / своя, обучена в `<путь>`>
- version pin: <revision / sha256 весов / дата снапшота API>
- license: <…>
- size / format: <8B, GGUF Q4_K_M, 5 GB>
- runtime: <vLLM / Ollama / llama.cpp / onnxruntime (DEP-<…>)>, hardware: <GPU 16 GB VRAM>
- weights stored: <BKT-<…> / nix store / кэш HF в `<путь>`>
- used by: SVC-<…> (PRM-<…>)
- quality: <метрика на каком наборе, текущее значение, порог>
- fallback: <что делаем без модели>
