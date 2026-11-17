"""Tests of the Einfluss table schema and ``pruefe_einfluss_frame``."""

from collections.abc import Callable
from datetime import UTC, datetime

import polars as pl
import pytest

import mdq_models.polars as mp
from mdq_models import EinflussResolution, Einflusswert, Groesse

MALO = "68283561554"
BERLIN = "Europe/Berlin"


def _ts(year: int, month: int, day: int, hour: int, minute: int = 0) -> datetime:
    return datetime(year, month, day, hour, minute, tzinfo=UTC)


def _frame(**columns: object) -> pl.DataFrame:
    data: dict[str, object] = {
        "malo": [MALO, MALO],
        "timestamp": [_ts(2026, 1, 1, 10), _ts(2026, 1, 1, 11)],
        "resolution": ["PT1H", "PT1H"],
        "groesse": ["temperatur", "temperatur"],
        "value": [1.5, 2.5],
        "quelle": ["dwd:01993", "dwd:01993"],
    }
    data.update(columns)
    return pl.DataFrame(data, schema=mp.EINFLUSS_DTYPES)


def test_einfluss_constants() -> None:
    assert mp.EINFLUSS_COLUMNS == tuple(Einflusswert.model_fields)
    assert list(mp.EINFLUSS_DTYPES) == list(mp.EINFLUSS_COLUMNS)
    assert mp.EINFLUSS_DTYPES["timestamp"] == mp.TIMESTAMP_DTYPE
    assert mp.EINFLUSS_DTYPES["value"] == pl.Float64
    assert mp.GROESSE_VALUES == tuple(g.value for g in Groesse)
    assert mp.GROESSE_VALUES == ("temperatur", "globalstrahlung")
    assert mp.EINFLUSS_RESOLUTION_VALUES == ("PT1H", "PT15M")
    assert (mp.COL_RESOLUTION, mp.COL_GROESSE, mp.COL_QUELLE) == ("resolution", "groesse", "quelle")
    assert mp.Groesse is Groesse
    assert mp.EinflussResolution is EinflussResolution


def test_pruefe_accepts_valid_and_empty() -> None:
    mp.pruefe_einfluss_frame(_frame())
    mp.pruefe_einfluss_frame(pl.DataFrame(schema=mp.EINFLUSS_DTYPES))
    reordered = _frame().select(reversed(mp.EINFLUSS_COLUMNS))
    mp.pruefe_einfluss_frame(reordered)


def test_pruefe_accepts_null_and_zero_values() -> None:
    mp.pruefe_einfluss_frame(_frame(value=[None, 0.0]))
    frame = _frame().with_columns(pl.lit(None).alias("value"))
    assert frame.schema["value"] == pl.Null
    mp.pruefe_einfluss_frame(frame)


def _t(h: int, m: int = 0) -> list[datetime]:
    return [_ts(2026, 1, 1, h, m), _ts(2026, 1, 1, 12)]


CASES = [
    pytest.param(lambda: _frame().drop("quelle"), "quelle", id="missing-column"),
    pytest.param(lambda: _frame().with_columns(pl.lit("x").alias("einheit")), "einheit", id="extra-column"),
    pytest.param(lambda: _frame().with_columns(pl.col("value").cast(pl.String)), "value", id="value-string"),
    pytest.param(
        lambda: _frame().with_columns(pl.col("timestamp").dt.convert_time_zone("UTC")), "timestamp", id="ts-utc"
    ),
    pytest.param(
        lambda: _frame().with_columns(pl.col("timestamp").dt.replace_time_zone(None)), "timestamp", id="ts-naive"
    ),
    pytest.param(
        lambda: _frame().with_columns(pl.col("timestamp").cast(pl.Datetime("ns", BERLIN))), "timestamp", id="ts-ns"
    ),
    pytest.param(lambda: _frame(quelle=["dwd:01993", None]), "quelle", id="null-quelle"),
    pytest.param(lambda: _frame(malo=["68283561555", "68283561555"]), "malo", id="bad-malo"),
    pytest.param(lambda: _frame(groesse=["cloud_cover", "temperatur"]), "groesse", id="bad-groesse"),
    pytest.param(lambda: _frame(resolution=["PT30M", "PT30M"]), "resolution", id="bad-resolution"),
    pytest.param(lambda: _frame(timestamp=_t(10, 15)), "timestamp", id="pt1h-quarter"),
    pytest.param(lambda: _frame(timestamp=_t(10, 7), resolution=["PT15M", "PT15M"]), "timestamp", id="pt15m-minute-7"),
    pytest.param(lambda: _frame(value=[float("nan"), 1.0]), "value", id="nan"),
    pytest.param(lambda: _frame(value=[float("inf"), 1.0]), "value", id="inf"),
    pytest.param(lambda: _frame(value=[1.0, -float("inf")]), "value", id="-inf"),
    pytest.param(lambda: _frame(value=[1e9, 1.0]), "value", id="too-large"),
    pytest.param(lambda: _frame(quelle=["dwd: 01993", "dwd:01993"]), "quelle", id="quelle-space"),
    pytest.param(lambda: _frame(quelle=["dwd:01993\n", "dwd:01993"]), "quelle", id="quelle-newline"),
    pytest.param(lambda: _frame(quelle=["", "dwd:01993"]), "quelle", id="quelle-empty"),
    pytest.param(lambda: _frame(quelle=["x" * 65, "dwd:01993"]), "quelle", id="quelle-long"),
    pytest.param(lambda: _frame(quelle=["dwd:ä", "dwd:01993"]), "quelle", id="quelle-nonascii"),
    pytest.param(lambda: _frame(timestamp=[_ts(2026, 1, 1, 10)] * 2), "timestamp", id="duplicate"),
    pytest.param(
        lambda: _frame(timestamp=[_ts(2026, 1, 1, 10), _ts(2026, 1, 1, 11, 15)], resolution=["PT1H", "PT15M"]),
        "resolution",
        id="mixed-resolution",
    ),
]


@pytest.mark.parametrize(("build", "column"), CASES)
def test_pruefe_rejects(build: Callable[[], pl.DataFrame], column: str) -> None:
    with pytest.raises(ValueError, match=column):
        mp.pruefe_einfluss_frame(build())


def test_pruefe_error_names_first_row() -> None:
    with pytest.raises(ValueError, match=r"68283561554.*2026-01-01") as exc:
        mp.pruefe_einfluss_frame(_frame(value=[1.0, float("nan")]))
    assert "value" in str(exc.value)


def test_pruefe_rejects_year_out_of_range() -> None:
    frame = _frame().with_columns(pl.col("timestamp").dt.offset_by("-200y"))
    with pytest.raises(ValueError, match="timestamp"):
        mp.pruefe_einfluss_frame(frame)


def test_pruefe_rejects_seconds() -> None:
    frame = _frame().with_columns(pl.col("timestamp").dt.offset_by("1s"))
    with pytest.raises(ValueError, match="timestamp"):
        mp.pruefe_einfluss_frame(frame)


def test_pruefe_rejects_lazyframe() -> None:
    with pytest.raises(TypeError):
        mp.pruefe_einfluss_frame(_frame().lazy())  # type: ignore[arg-type]
