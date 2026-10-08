"""Regenerate immutable Python-oracle fixtures; never used by wk at runtime."""
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
MUTATIONS = {
    "default": {},
    "strict": {},
    "duplicate": {"docs/system/domain.md": "\n## ENT-order — Дубль\nstatus: draft\n"},
    "reference": {"docs/system/domain.md": "\nENT-missing\n"},
    "draft": {"docs/system/domain.md": "\n## ENT-draft — Черновик\nstatus: draft\n", "src/draft.go": "// doc: ENT-draft\n"},
    "header": {"docs/system/domain.md": "\n| ENT-noheader | draft |\n"},
    "budget": {"docs/INDEX.md": "\n" * 151},
}
for name, additions in MUTATIONS.items():
    with tempfile.TemporaryDirectory() as temp:
        project = Path(temp) / "project"
        shutil.copytree(ROOT / "testdata/example", project)
        for path, text in additions.items():
            file = project / path
            file.parent.mkdir(parents=True, exist_ok=True)
            with file.open("a") as out:
                out.write(text)
        args = [sys.executable, str(ROOT / "testdata/oracle/check.py"), "--docs", str(project / "docs"), "--code", str(project)]
        if name == "strict":
            args.append("--strict")
        result = subprocess.run(args, capture_output=True, check=False)
        assert not result.stderr, result.stderr
        dest = ROOT / "testdata/golden" / name
        dest.mkdir(parents=True, exist_ok=True)
        (dest / "stdout.txt").write_bytes(result.stdout)
        (dest / "exit.txt").write_text(str(result.returncode))
        (dest / "mutation.json").write_text(json.dumps(additions, ensure_ascii=False, indent=2))
        shutil.copyfile(project / "docs/_meta/registry.md", dest / "registry.md")
