"""Run inside backend's Nix devShell to freeze the real server export result."""
import json
from pathlib import Path
import tempfile

from wikisvc.services.policies import export_block

root = Path(__file__).resolve().parents[1] / "testdata/policy"
root.mkdir(exist_ok=True)
compiled = {"version": "b7d2e104", "markdown": "# Политики\n- [MUST] Используй Go. → rule-go\n- [SHOULD] Пиши тесты. → rule-tests\n\t \u0085\u001c", "over_budget": True, "used_tokens": 2500}
(root / "compiled.json").write_text(json.dumps(compiled, ensure_ascii=False))
for name, current in {
    "empty": "",
    "text": "# Project\nUnchanged text\n",
    "crlf": "# Project\r\nUnchanged text\r",
    "replace": "Before\n<!-- wikisvc:begin version=a3f91c2e -->\nOld\n<!-- wikisvc:end -->\nAfter\n",
}.items():
    with tempfile.TemporaryDirectory() as tmp:
        path = Path(tmp) / "AGENTS.md"
        path.write_text(current)
        export_block(path, compiled)
        (root / (name + ".before")).write_text(current)
        (root / (name + ".after")).write_bytes(path.read_bytes())
