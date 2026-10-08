# Проверка реализации 02, 06 и 07

Текущий полный прогон backend: **260 passed**, покрытие измеряемых модулей
**97.14%**; новый `docs_audit` — **100%**, весь Go CLI — **100% каждой функции**.
Детали 06/07 находятся в последнем разделе. Ниже сохранён исходный протокол 02.

Платформа: x86_64-linux. Go: 1.26.7 из зафиксированного nixpkgs.

## CLI

`nix develop -c just lint`:

```text
test -z "$(gofmt -l cmd internal)"
go vet ./...
```

`nix develop -c just test`: все Go-функции, включая `main`, имеют 100% покрытия
операторов. Полный локальный отчёт — `coverage-summary.txt` (генерируется,
в Git не хранится). Итог `go tool cover -func=coverage.out`:

```text
total:                                      (statements)    100.0%
```

`nix develop -c env CGO_ENABLED=1 go test -race ./...`:

```text
ok  	wk-cli/cmd/cli	1.008s
ok  	wk-cli/internal/wk	3.355s
```

`nix build .#wk` успешен. Проверка бинаря:

```text
result/bin/wk: ELF 64-bit LSB executable, x86-64, version 1 (SYSV), statically linked, not stripped
```

`nix run .#wk -- --help` выводит список команд. `nix flake check`:

```text
all checks passed!
```

Остальные объявленные в flake системы не запускались на этом Linux-хосте.

## Backend

`nix develop -c just lint`:

```text
All checks passed!
Success: no issues found in 95 source files
```

`nix develop -c pytest --cov --cov-report=term-missing`:

```text
src/wikisvc/domain/project_templates.py      66      0   100%
TOTAL                                      1450     47    97%
Required test coverage of 85.0% reached. Total coverage: 96.76%
================== 249 passed, 1 warning in 727.61s (0:12:07) ==================
```

Предупреждение — существующий `StarletteDeprecationWarning` о httpx в
fastapi.testclient; базовый прогон до изменений также содержал его.
Новые тесты шаблонов: 34. Общий исходный прогон: 215 тестов.

`nix flake check` backend, включая пакет сервиса, UI и deployment:

```text
all checks passed!
```

## Настоящий HTTP-контракт

Собранный Nix-бинарь проверен на временном экземпляре настоящего backend
с отдельными wiki/state/index, тестовыми страницами и временным токеном writer.
Временный сервер и его данные удалены после проверки. Результат:

```text
PASS wk init --dir
PASS generated flake devShell contains wk
PASS wk doctor
PASS wk component add
PASS wk template list
PASS wk template show
PASS wk policy search
PASS wk propose --file
PASS real backend contract, draft→submitted, no token on disk
```

Graphify: граф построен локальным AST-анализом в `graphify-out/graph.json`;
проверены связи валидации, файловых записей, HTTP-клиента и proposal flow.
Context7 использован для YAML KnownFields и Nix buildGoModule.

## Задачи 06 и 07 — 2026-10-09

Реализованы встроенная проверка docs, upgrade, синхронизация/сверка политик и
семантический docs_audit в backend с запуском из UI. Хуки и skills (03/04)
принадлежат отдельной параллельной работе.

- Семь Python golden-сценариев: побайтовое совпадение stdout/registry и exit code.
- Четыре export golden-сценария: пустой файл, текст, CRLF, замена блока;
  результат настоящего `wikisvc.services.policies.export_block`.
- `nix develop -c just lint`, `nix develop -c just test`: зелёные, все Go-функции 100%.
- `nix develop -c env CGO_ENABLED=1 go test -race ./...`: все три пакета прошли.
- `nix build .#wk`, `nix flake check` для CLI: зелёные.
- Новый `wikiagent.jobs.docs_audit`: 160 statements, 0 missing, 100% coverage;
  11 интеграционных и модульных тестов, включая HTTP/auth, budgets, секреты,
  отмену/resume, потерю ответа после POST и сохранение счётчика вызовов.
- Browser Playwright: загрузка docs → audit → findings, `1 passed (19.6s)`.
- Backend/UI: ruff, mypy, Prettier, TypeScript и Vite build прошли.

Собранный Nix-бинарь на мигрированном docs-kit/example:

```text
ID: 76, ссылок: 304, маркеров в коде: 24, ошибок: 0, предупреждений: 0
```

Graphify обновлён в wk и использован для трассировки docsReport → Check и
compilePolicies/exportPolicy. Context7: Pydantic validators/defaults.

Полный backend: `nix develop -c pytest --cov --cov-report=term-missing`:

```text
TOTAL                                      1608     46    97%
Required test coverage of 85.0% reached. Total coverage: 97.14%
260 passed, 1 warning in 546.85s (0:09:06)
```

Предупреждение — прежний StarletteDeprecationWarning о httpx.

Финальный `nix flake check` backend (сервис, UI, deployment), включая правку
контрольной точки со счётчиком вызовов:

```text
all checks passed!
```

Проверена текущая платформа x86_64-linux; остальные системы не собирались.
Offline init собранным Nix-бинарём на временном проекте: check.py отсутствует,
workflow и AGENTS содержат `wk check` / `wk check --strict`; временные данные удалены.

В `../hooks/guard-checker/claude/settings.json` уже есть вызов
`wk hook guard-paths`, однако команды в wk пока нет: интеграционная приёмка
защиты check.py зависит от отдельно выполняемой задачи 04.

## Уточнение 07: область находки — 2026-10-09

Findings получает `scope`; docs_audit передаёт имя проекта. Область входит
в отпечаток только при непустом значении. Миграция добавляет колонку с `DEFAULT ''`,
сохраняя существующие строки, отпечатки и dismiss. Статистика остаётся по kind,
доступ — по страницам правил. Неверный scope обрабатывается общим обработчиком
Pydantic: `400 / E_REQUEST_INVALID`, включая запрос с неверными scope и kind.

Пять новых регрессионных тестов проверяют отдельные ID/данные/статусы проектов,
дедупликацию внутри проекта, независимость dismiss, фильтрацию и пагинацию,
смену версии правила, миграцию старой базы и её повторное применение, валидацию
и два полноценных аудита. `tests/test_agent_heal.py` не изменён и проходит.
Для пустой области зафиксирован отпечаток, полученный от кода до изменения:

```text
f0aa1a9a60b7a4e23133f7b380d8d59399c507a14efdf1672e7a9d0092965e6d
```

`nix develop -c just lint`:

```text
ruff check .
All checks passed!
ruff format --check .
208 files already formatted
mypy src
Success: no issues found in 96 source files
```

`nix develop -c pytest --cov --cov-report=term-missing`:

```text
src/wikiagent/jobs/docs_audit.py            160      0   100%
TOTAL                                      1608     46    97%
Required test coverage of 85.0% reached. Total coverage: 97.14%
================== 265 passed, 1 warning in 550.81s (0:09:10) ==================
```

Это настроенный в pyproject.toml coverage-набор. Предупреждение — прежний
StarletteDeprecationWarning о httpx. `docs_audit` покрыт на 100%.

`nix flake check --print-build-logs`, включая сервис, UI и deployment:

```text
wikisvc> Required test coverage of 85.0% reached. Total coverage: 97.14%
wikisvc> ================== 265 passed, 1 warning in 556.91s (0:09:16) ==================
all checks passed!
```

Проверена x86_64-linux. Graphify использован по графу wk; Context7 — для
Pydantic Field/pattern/default и extra="forbid". Общий обработчик ошибок,
машина состояний, visible, decide, stats, проверки цитат, секретов и размера
не изменены. После проверок код backend не менялся.
