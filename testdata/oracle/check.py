#!/usr/bin/env python3
"""Проверка docs/ и генерация _meta/registry.md.

Запуск из корня репозитория:
    python3 docs/_meta/check.py            # проверить и обновить реестр
    python3 docs/_meta/check.py --strict   # предупреждения = ошибки (для CI)

Только стандартная библиотека Python 3.9+.
"""
from __future__ import annotations

import argparse
import os
import re
import sys
from collections import defaultdict
from dataclasses import dataclass, field
from pathlib import Path

# ---------------------------------------------------------------- правила

# префикс -> где разрешено определять.
#   "path/file.md"           — конкретный файл внутри docs/
#   ("component", "x.md")    — components/<c>/x.md
#   ("dir", "decisions/")    — любой файл в папке
LOCATION: dict[str, object] = {
    "ACT": "system/overview.md", "EXT": "system/overview.md",
    "TERM": "system/glossary.md",
    "ENT": "system/domain.md", "STATE": "system/domain.md", "INV": "system/domain.md",
    "FLOW": "system/flows.md",
    "ROLE": "system/security.md", "PERM": "system/security.md", "SEC": "system/security.md",
    "ERR": "system/errors.md", "NFR": "system/nfr.md",
    "SVC": ("component", "component.md"), "LIB": ("component", "component.md"),
    "MOD": ("component", "code.md"), "CLS": ("component", "code.md"),
    "API": ("component", "contracts.md"), "EVT": ("component", "contracts.md"),
    "CLI": ("component", "contracts.md"), "DTO": ("component", "contracts.md"),
    "FMT": ("component", "contracts.md"), "PRM": ("component", "ai.md"),
    "TBL": ("component", "data.md"), "KV": ("component", "data.md"), "BKT": ("component", "data.md"),
    "JOB": ("component", "runtime.md"), "CFG": ("component", "runtime.md"),
    "LANG": "stack/languages.md", "TOOL": "stack/languages.md",
    "DEP": "stack/dependencies.md", "MDL": "stack/models.md",
    "RES": "infra/resources.md", "ENV": "infra/resources.md",
    "PIPE": "infra/delivery.md", "OBS": "infra/observability.md",
    "ADR": ("dir", "decisions/"),
    "Q": "open-questions.md", "RISK": "risks.md",
}
PREFIXES = sorted(LOCATION, key=len, reverse=True)

GROUPS = [
    ("Система и домен", ["ACT", "EXT", "TERM", "ENT", "STATE", "INV", "FLOW", "NFR"]),
    ("Безопасность и ошибки", ["ROLE", "PERM", "SEC", "ERR"]),
    ("Компоненты и код", ["SVC", "LIB", "MOD", "CLS"]),
    ("Контракты", ["API", "EVT", "CLI", "DTO", "FMT", "PRM"]),
    ("Данные", ["TBL", "KV", "BKT"]),
    ("Рантайм", ["JOB", "CFG"]),
    ("Стек", ["LANG", "TOOL", "DEP", "MDL"]),
    ("Инфраструктура", ["RES", "ENV", "PIPE", "OBS"]),
    ("Решения и незакрытое", ["ADR", "Q", "RISK"]),
]

STATUS_DEFAULT = {"draft", "planned", "implemented", "deprecated"}
STATUS_BY_PREFIX = {
    "ADR": {"proposed", "accepted", "superseded"},
    "Q": {"open", "resolved"},
    "RISK": {"open", "accepted", "closed"},
    "TERM": None,  # статус не нужен
}
OWNED = {"API", "EVT", "CLI", "DTO", "FMT", "PRM", "TBL", "KV", "BKT", "MOD", "CLS", "JOB", "CFG"}
CODE_TYPES = OWNED  # у этих типов в коде должен быть маркер `doc: <ID>`

BUDGET_INDEX, BUDGET_COMPONENT, BUDGET_OTHER = 150, 120, 400

# из какой колонки брать название для строк-определений (первая найденная)
TITLE_COLUMNS = [
    "название", "термин", "кто", "роль", "право", "класс", "когда возникает", "требование",
    "вопрос", "что", "пакет", "язык и версия", "инструмент", "назначение", "имя / путь", "на что влияет",
]

SKIP_CODE_DIRS = {
    ".git", ".hg", "node_modules", "target", ".venv", "venv", "__pycache__", "dist",
    "build", "vendor", ".direnv", "result", ".mypy_cache", ".pytest_cache", ".next", ".turbo",
}

# ---------------------------------------------------------------- регулярки

_P = "|".join(PREFIXES)
_NAME = r"[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?"
# ссылка: ID, необязательная часть #x, необязательный хвост .* (группа)
REF_RE = re.compile(rf"(?<![\w-])((?:{_P})-{_NAME})(\.\*)?(#[A-Za-z0-9_.-]+)?")
FULL_ID_RE = re.compile(rf"^(?:{_P})-{_NAME}$")
HEADING_RE = re.compile(r"^(#{1,6})\s+(.*?)\s*$")
HEAD_DEF_RE = re.compile(rf"^((?:{_P})-{_NAME})(?:\s+[—–-]\s+(.*))?$")
MARKER_RE = re.compile(r"(?://|#|--|/\*|<!--|;)\s*doc:\s*(.+)")
CELL_SPLIT_RE = re.compile(r"(?<!\\)\|")


def prefix_of(id_: str) -> str:
    return id_.split("-", 1)[0]


# ---------------------------------------------------------------- модель

@dataclass
class Definition:
    id: str
    title: str
    file: str
    line: int
    meta: dict
    kind: str  # "card" | "row"


@dataclass
class Ref:
    target: str       # базовый ID или "PREFIX-scope.*"
    wildcard: bool
    file: str
    line: int
    user: str         # ID карточки, внутри которой ссылка, или путь файла


@dataclass
class Report:
    errors: list = field(default_factory=list)
    warnings: list = field(default_factory=list)

    def err(self, where: str, msg: str):
        self.errors.append(f"{where}: {msg}")

    def warn(self, where: str, msg: str):
        self.warnings.append(f"{where}: {msg}")


# ---------------------------------------------------------------- разбор docs

def doc_files(docs: Path):
    for p in sorted(docs.rglob("*.md")):
        rel = p.relative_to(docs)
        if any(part.startswith("_") for part in rel.parts[:-1]):
            continue  # _meta, components/_template
        if "template" in p.name.lower():
            continue
        yield p, rel.as_posix()


def parse_meta(line: str) -> dict:
    meta = {}
    for chunk in line.split(" | "):
        if ":" not in chunk:
            return {}
        k, v = chunk.split(":", 1)
        k = k.strip().strip("`").lower()
        if not re.fullmatch(r"[a-z_]+", k):
            return {}
        meta[k] = v.strip().strip("`")
    return meta


def split_row(line: str) -> list[str]:
    s = line.strip()
    if s.startswith("|"):
        s = s[1:]
    if s.endswith("|") and not s.endswith("\\|"):
        s = s[:-1]
    return [c.strip().strip("`").strip() for c in CELL_SPLIT_RE.split(s)]


def parse_docs(docs: Path, rep: Report):
    defs: dict[str, Definition] = {}
    refs: list[Ref] = []
    line_counts: dict[str, int] = {}

    for path, rel in doc_files(docs):
        lines = path.read_text(encoding="utf-8").splitlines()
        line_counts[rel] = len(lines)
        card: tuple[str, int] | None = None   # (id, уровень заголовка)
        table_headers: list[str] | None = None
        in_fence = False

        for i, raw in enumerate(lines):
            ln = i + 1
            stripped = raw.strip()
            skip_ids_in_line: set[str] = set()
            row_user: str | None = None

            if stripped.startswith("```"):
                in_fence = not in_fence

            # --- заголовки
            m = HEADING_RE.match(raw) if not in_fence else None
            if m:
                level, text = len(m.group(1)), m.group(2)
                if card and level <= card[1]:
                    card = None
                hd = HEAD_DEF_RE.match(text)
                if hd and level <= 4:
                    id_ = hd.group(1)
                    meta = {}
                    for nxt in lines[i + 1:]:
                        if nxt.strip():
                            meta = parse_meta(nxt.strip())
                            break
                    add_def(defs, Definition(id_, hd.group(2) or "", rel, ln, meta, "card"), rep)
                    card = (id_, level)
                    skip_ids_in_line.add(id_)
                table_headers = None

            # --- таблицы
            elif stripped.startswith("|") and not in_fence:
                cells = split_row(stripped)
                if table_headers is None:
                    table_headers = [c.lower() for c in cells]
                    if cells and FULL_ID_RE.match(cells[0]):
                        rep.err(f"{rel}:{ln}", f"{cells[0]}: строка похожа на определение, но у таблицы нет "
                                               f"заголовка с колонкой id (лишняя пустая строка?)")
                        skip_ids_in_line.add(cells[0])
                elif all(re.fullmatch(r":?-{2,}:?", c) for c in cells if c):
                    pass  # строка-разделитель
                elif table_headers and table_headers[0] == "id" and cells and FULL_ID_RE.match(cells[0]):
                    id_ = cells[0]
                    meta = {h: cells[j] for j, h in enumerate(table_headers)
                            if j < len(cells) and h in ("status", "owner", "replaced_by")}
                    title = next((cells[table_headers.index(h)] for h in TITLE_COLUMNS
                                  if h in table_headers and table_headers.index(h) < len(cells)),
                                 cells[1] if len(cells) > 1 else "")
                    add_def(defs, Definition(id_, title, rel, ln, meta, "row"), rep)
                    skip_ids_in_line.add(id_)
                    row_user = id_
            else:
                table_headers = None

            # --- ссылки
            user = row_user or (card[0] if card else rel)
            for rm in REF_RE.finditer(raw):
                base, star = rm.group(1), bool(rm.group(2))
                if base in skip_ids_in_line and not star:
                    skip_ids_in_line.discard(base)  # само определение
                    continue
                if base == user and not star:
                    continue  # ссылка карточки на саму себя (например TBL-x#col)
                refs.append(Ref(base + (".*" if star else ""), star, rel, ln, user))

    return defs, refs, line_counts


def add_def(defs, d: Definition, rep: Report):
    if d.id in defs:
        o = defs[d.id]
        rep.err(f"{d.file}:{d.line}", f"{d.id} уже определён в {o.file}:{o.line}")
        return
    defs[d.id] = d


# ---------------------------------------------------------------- проверки

def expected_location(prefix: str, rel: str) -> str | None:
    """None — место правильное, иначе текст ожидания."""
    rule = LOCATION[prefix]
    if isinstance(rule, str):
        return None if rel == rule else rule
    kind, target = rule
    if kind == "dir":
        return None if rel.startswith(target) else target + "*.md"
    parts = rel.split("/")
    if len(parts) == 3 and parts[0] == "components" and parts[2] == target:
        return None
    return f"components/<c>/{target}"


def check_defs(defs: dict[str, Definition], rep: Report):
    component_of_folder: dict[str, str] = {}
    for d in defs.values():
        if prefix_of(d.id) in ("SVC", "LIB") and d.file.startswith("components/"):
            component_of_folder[d.file.split("/")[1]] = d.id

    for d in defs.values():
        p = prefix_of(d.id)
        where = f"{d.file}:{d.line}"

        exp = expected_location(p, d.file)
        if exp:
            rep.warn(where, f"{d.id}: тип {p} определяется в {exp}")

        allowed = STATUS_BY_PREFIX.get(p, STATUS_DEFAULT)
        st = d.meta.get("status", "")
        if allowed is not None:
            if not st:
                rep.err(where, f"{d.id}: нет status (мета-строка сразу под заголовком или колонка status)")
            elif st not in allowed:
                rep.err(where, f"{d.id}: status '{st}' не из {sorted(allowed)}")

        rb = d.meta.get("replaced_by", "")
        if rb and rb not in defs:
            rep.err(where, f"{d.id}: replaced_by → {rb} не определён")

        if p in OWNED:
            owner = d.meta.get("owner", "")
            if not owner:
                rep.err(where, f"{d.id}: нет owner (SVC-… или LIB-…)")
            elif owner not in defs or prefix_of(owner) not in ("SVC", "LIB"):
                rep.err(where, f"{d.id}: owner '{owner}' — не определённый SVC-/LIB-")
            elif d.file.startswith("components/"):
                folder_owner = component_of_folder.get(d.file.split("/")[1])
                if folder_owner and folder_owner != owner:
                    rep.warn(where, f"{d.id}: owner {owner}, но файл лежит в папке {folder_owner}")


def check_refs(defs, refs: list[Ref], rep: Report):
    for r in refs:
        where = f"{r.file}:{r.line}"
        if r.wildcard:
            stem = r.target[:-2]
            if not any(k.startswith(stem + ".") or k.startswith(stem + "-") for k in defs):
                rep.err(where, f"{r.target}: под шаблон не подходит ни один ID")
        elif r.target not in defs:
            rep.err(where, f"ссылка на неопределённый {r.target}")


def check_budget(line_counts: dict[str, int], rep: Report):
    for rel, n in line_counts.items():
        if rel == "INDEX.md":
            limit = BUDGET_INDEX
        elif rel.startswith("components/") and rel.endswith("/component.md"):
            limit = BUDGET_COMPONENT
        else:
            limit = BUDGET_OTHER
        if n > limit:
            rep.warn(rel, f"{n} строк при бюджете {limit} — раздели файл")


# ---------------------------------------------------------------- код

def scan_code(code_root: Path, docs: Path, defs, rep: Report):
    markers: dict[str, list[str]] = defaultdict(list)
    docs_abs = docs.resolve()
    for dirpath, dirnames, filenames in os.walk(code_root):
        dp = Path(dirpath)
        if dp.resolve() == docs_abs:
            dirnames[:] = []
            continue
        dirnames[:] = [d for d in dirnames if d not in SKIP_CODE_DIRS
                       and (dp / d).resolve() != docs_abs]
        for fn in filenames:
            fp = dp / fn
            try:
                if fp.stat().st_size > 2_000_000:
                    continue
                data = fp.read_bytes()
            except OSError:
                continue
            if b"\0" in data[:4096] or b"doc:" not in data:
                continue
            text = data.decode("utf-8", errors="ignore")
            rel = fp.relative_to(code_root).as_posix()
            for i, line in enumerate(text.splitlines(), 1):
                mm = MARKER_RE.search(line)
                if not mm:
                    continue
                for rm in REF_RE.finditer(mm.group(1)):
                    id_ = rm.group(1)
                    loc = f"{rel}:{i}"
                    if id_ not in defs:
                        rep.err(loc, f"маркер doc: {id_} — такого ID нет в docs/")
                        continue
                    markers[id_].append(loc)

    for id_, d in defs.items():
        p = prefix_of(id_)
        st = d.meta.get("status", "")
        where = f"{d.file}:{d.line}"
        has = bool(markers.get(id_))
        if p in CODE_TYPES and st == "implemented" and not has:
            rep.warn(where, f"{id_}: implemented, но в коде нет маркера `doc: {id_}`")
        if has and st in ("draft", "planned"):
            rep.warn(where, f"{id_}: status {st}, но в коде уже есть маркер ({markers[id_][0]}) — обнови статус")
        if has and st == "deprecated":
            rep.warn(where, f"{id_}: deprecated, но ещё используется в коде ({markers[id_][0]})")
    return markers


# ---------------------------------------------------------------- реестр

def write_registry(path: Path, defs, refs, markers):
    users: dict[str, list[str]] = defaultdict(list)
    for r in refs:
        targets = ([k for k in defs if k.startswith(r.target[:-2] + ".")] if r.wildcard else [r.target])
        for t in targets:
            if r.user != t and r.user not in users[t]:
                users[t].append(r.user)

    def short(items, n=6):
        items = [f"`{x}`" if "/" in x else x for x in items]
        return ", ".join(items[:n]) + (f" +{len(items) - n}" if len(items) > n else "")

    by_status = defaultdict(int)
    for d in defs.values():
        by_status[d.meta.get("status") or "—"] += 1

    out = [
        "# Реестр ID",
        "",
        "> Сгенерировано `docs/_meta/check.py`. Руками не править — перезапусти скрипт.",
        "> Колонки: где определён, где в коде (маркер `doc:`), кто ссылается.",
        "",
        "Всего ID: **%d** · " % len(defs) + " · ".join(f"{k}: {v}" for k, v in sorted(by_status.items())),
        "",
    ]
    for gname, prefixes in GROUPS:
        rows = [d for d in defs.values() if prefix_of(d.id) in prefixes]
        if not rows:
            continue
        rows.sort(key=lambda d: (prefixes.index(prefix_of(d.id)), d.id))
        out += [f"## {gname}", "",
                "| id | название | status | owner | определён | в коде | используют |",
                "|---|---|---|---|---|---|---|"]
        for d in rows:
            title = d.title.replace("|", "\\|")
            out.append(
                f"| {d.id} | {title} | {d.meta.get('status', '')} | {d.meta.get('owner', '')} "
                f"| `{d.file}:{d.line}` | {short(markers.get(d.id, []), 3)} | {short(users.get(d.id, []))} |"
            )
        out.append("")
    path.write_text("\n".join(out), encoding="utf-8")


# ---------------------------------------------------------------- main

def main() -> int:
    here = Path(__file__).resolve().parent
    ap = argparse.ArgumentParser(description="Проверка docs/ и генерация реестра ID")
    ap.add_argument("--docs", type=Path, default=here.parent, help="папка docs (по умолчанию — родитель _meta)")
    ap.add_argument("--code", type=Path, default=None, help="корень кода (по умолчанию — родитель docs)")
    ap.add_argument("--strict", action="store_true", help="предупреждения считать ошибками")
    ap.add_argument("--no-registry", action="store_true", help="не перезаписывать _meta/registry.md")
    args = ap.parse_args()

    docs = args.docs.resolve()
    code = (args.code or docs.parent).resolve()
    rep = Report()

    defs, refs, line_counts = parse_docs(docs, rep)
    check_defs(defs, rep)
    check_refs(defs, refs, rep)
    check_budget(line_counts, rep)
    markers = scan_code(code, docs, defs, rep)

    if not args.no_registry:
        (docs / "_meta").mkdir(exist_ok=True)
        write_registry(docs / "_meta" / "registry.md", defs, refs, markers)

    for e in rep.errors:
        print(f"ОШИБКА  {e}")
    for w in rep.warnings:
        print(f"ВНИМАНИЕ {w}")
    print(f"\nID: {len(defs)}, ссылок: {len(refs)}, маркеров в коде: {sum(len(v) for v in markers.values())}, "
          f"ошибок: {len(rep.errors)}, предупреждений: {len(rep.warnings)}")

    if rep.errors or (args.strict and rep.warnings):
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
