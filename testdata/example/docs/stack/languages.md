# Языки, рантаймы, инструменты

| id | язык и версия | где закреплена версия | компоненты | почему | status |
|---|---|---|---|---|---|
| LANG-go1.23 | Go 1.23 | `flake.nix` | SVC-orders | ADR-0001 | implemented |
| LANG-python3.12 | Python 3.12 | `flake.nix` | SVC-recs | ADR-0002 | implemented |

| id | инструмент | для чего | версия / где закреплена | status |
|---|---|---|---|---|
| TOOL-nix | Nix flakes | окружение разработки, сборка, деплой | `flake.lock` | implemented |
| TOOL-uv | uv | зависимости Python внутри nix-окружения | `flake.lock` | implemented |

Всё окружение: `nix develop`.
