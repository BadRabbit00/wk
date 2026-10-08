# Сборка и выкатка

### PIPE-<имя> — <Что делает пайплайн>
status: <…>

- trigger: <push в main / тег v* / PR>
- where: <GitHub Actions `.github/workflows/<…>.yml` / Hydra / …>
- steps: <lint → test → `wk check --strict` → build → deploy в ENV-<…>>
- artifacts: <образ в `<registry>`, nix-closure>
- rollback: <как откатить>
- secrets: <какие CFG-<…> нужны пайплайну>
