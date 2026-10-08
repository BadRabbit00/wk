# recs: LLM и промпты

### PRM-recs.explain — Объяснение, почему товар рекомендован
status: draft | owner: SVC-recs

- model: ещё не выбрана (Q-recs.llm)
- inputs: `{skus}` — последние покупки, `{candidate}` — рекомендованный SKU
- output: одна фраза до 120 символов, русский язык
- validation: длина и отсутствие цен в тексте
- failure: показываем рекомендацию без объяснения
