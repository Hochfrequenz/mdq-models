"""The Python snippets of README.md run as written, so the examples cannot rot."""

import re
from pathlib import Path

import pytest

README = Path(__file__).resolve().parent.parent / "README.md"
# A fenced block whose info string is "python" (always run) or "python polars" (needs the extra).
BLOCK = re.compile(r"^```python( polars)?\n(.*?)^```$", re.DOTALL | re.MULTILINE)


def test_readme_python_snippets_run(capsys: pytest.CaptureFixture[str]) -> None:
    blocks = BLOCK.findall(README.read_text(encoding="utf-8"))
    assert [needs_polars for needs_polars, _ in blocks] == ["", " polars"]
    namespace: dict[str, object] = {}  # shared: the Polars snippet continues the first one
    exec(compile(blocks[0][1], "<README.md python>", "exec"), namespace)
    printed = capsys.readouterr().out.splitlines()
    assert printed[0] == "51238696781 2026-03-29T00:45:00+00:00 1.5 wahr"
    assert printed[1] == (
        '{"malo":"51238696781","timestamp":"2026-03-29T01:00:00Z","resolution":"PT15M","value":"2.5",'
        '"status":"ersatzwert","herkunft":"mqa","profiltyp":"haushalt","richtung":"bezug"}'
    )
    assert printed[2] == "rejected"
    pytest.importorskip("polars")
    exec(compile(blocks[1][1], "<README.md python polars>", "exec"), namespace)
    assert capsys.readouterr().out.strip() == "2026-03-29 01:45:00+01:00"
