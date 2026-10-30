"""Import behaviour of ``mdq_models.polars``. No ``importorskip``: these tests run in the job without the extra too."""

import importlib.util
import subprocess
import sys

import pytest

POLARS_INSTALLED = importlib.util.find_spec("polars") is not None


def _run(code: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run([sys.executable, "-c", code], capture_output=True, text=True, check=False)


def test_import_error_names_the_extra_while_mdq_models_still_imports() -> None:
    """Works with and without polars installed: a child process in which polars cannot be imported."""
    code = (
        "import sys\n"
        "sys.modules['polars'] = None\n"  # makes `import polars` raise ImportError, as if it were not installed
        "import mdq_models\n"
        "try:\n"
        "    import mdq_models.polars\n"
        "except ImportError as exc:\n"
        "    assert 'mdq-models[polars]' in str(exc), str(exc)\n"
        "else:\n"
        "    raise SystemExit('no ImportError')\n"
    )
    result = _run(code)
    assert result.returncode == 0, result.stderr + result.stdout


@pytest.mark.skipif(POLARS_INSTALLED, reason="only meaningful where polars is really missing")
def test_real_missing_polars_raises_import_error_naming_the_extra() -> None:
    import mdq_models  # noqa: F401, PLC0415 - must work without polars

    with pytest.raises(ImportError, match=r"mdq-models\[polars\]"):
        importlib.import_module("mdq_models.polars")


def test_mdq_models_does_not_import_polars() -> None:
    result = _run("import sys, mdq_models\nraise SystemExit(1 if 'polars' in sys.modules else 0)\n")
    assert result.returncode == 0, result.stderr
