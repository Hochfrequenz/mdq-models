"""Polars table schema (``mdq_models.polars``); the tests that need polars are skipped only if it is not installed."""

import importlib
from collections.abc import Iterator
from datetime import datetime, timedelta
from decimal import Context, Decimal, Inexact, InvalidOperation, Rounded, localcontext
from pathlib import Path
from zoneinfo import ZoneInfo

import pytest

import mdq_models as mdq_models_pkg
from mdq_models import Herkunft, Messwert, Profiltyp, Richtung, Status

TESTDATA = Path(__file__).resolve().parent.parent / "testdata"
FIXTURES = sorted((TESTDATA / "messwert" / "valid").glob("*.json"))
LIMIT = Decimal(10) ** 9


def _row(value: str | None = "1.5", **changes: str | None) -> Messwert:
    fields: dict[str, str | None] = {
        "malo": "51238696781",
        "timestamp": "2026-01-15T10:15:00Z",
        "resolution": "PT15M",
        "value": value,
        "status": "wahr" if value is not None else "fehlend",
        "herkunft": "fremd" if value is not None else None,
        "profiltyp": "haushalt",
        "richtung": "bezug",
    }
    fields.update(changes)
    return Messwert.model_validate(fields)


pl = pytest.importorskip("polars")
mp = importlib.import_module("mdq_models.polars")


def test_columns_are_messwert_fields_except_resolution() -> None:
    fields = set(Messwert.model_fields)
    # `resolution` is the one pinned exception: the table knows only the 15-minute raster (mp.RESOLUTION_MINUTES),
    # so the column would always hold "PT15M". to_frame drops it and from_frame adds it back.
    assert fields - set(mp.CANONICAL_COLUMNS) == {"resolution"}
    assert set(mp.CANONICAL_COLUMNS) - fields == set()
    assert len(mp.CANONICAL_COLUMNS) == 7
    assert [f for f in Messwert.model_fields if f != "resolution"] == list(mp.CANONICAL_COLUMNS)
    assert list(mp.CANONICAL_DTYPES) == list(mp.CANONICAL_COLUMNS)
    assert mp.RESOLUTION_MINUTES == 15


def test_constants() -> None:
    assert mp.TIMESTAMP_DTYPE == pl.Datetime("us", "Europe/Berlin")
    assert mp.BERLIN_TZ == "Europe/Berlin"
    assert mp.CANONICAL_DTYPES["value"] == pl.Float64
    assert all(mp.CANONICAL_DTYPES[c] == pl.String for c in ("malo", "status", "herkunft", "profiltyp", "richtung"))
    for name in ("MALO", "TIMESTAMP", "VALUE", "STATUS", "HERKUNFT", "PROFILTYP", "RICHTUNG"):
        assert getattr(mp, f"COL_{name}") == name.lower()


def test_enum_tuples_equal_enum_values() -> None:
    assert mp.STATUS_VALUES == tuple(s.value for s in Status)
    assert mp.HERKUNFT_VALUES == tuple(s.value for s in Herkunft)
    assert mp.PROFILTYP_VALUES == tuple(s.value for s in Profiltyp)
    assert mp.RICHTUNG_VALUES == tuple(s.value for s in Richtung)
    assert mp.STATUS_VALUES == ("wahr", "vorlaeufig", "ersatzwert", "fehlend")


def test_reexports() -> None:
    assert mp.Status is mdq_models_pkg.Status
    assert mp.is_valid_malo_id is mdq_models_pkg.is_valid_malo_id
    assert mp.malo_check_digit is mdq_models_pkg.malo_check_digit
    assert mp.herkunft_passt_zu_status is mdq_models_pkg.herkunft_passt_zu_status
    assert mp.MALO_ID_LENGTH == 11


@pytest.mark.parametrize("path", FIXTURES, ids=lambda p: p.name)
def test_round_trip_over_valid_fixtures(path: Path) -> None:
    row = Messwert.model_validate_json(path.read_bytes())
    if row.value is not None and abs(row.value) >= LIMIT:
        with pytest.raises(ValueError, match="10"):
            mp.to_frame([row])
        return
    frame = mp.to_frame([row])
    assert frame.columns == list(mp.CANONICAL_COLUMNS)
    assert dict(frame.schema) == mp.CANONICAL_DTYPES
    assert mp.from_frame(frame) == [row]


def _day(date: str) -> list[Messwert]:
    start = datetime.fromisoformat(date + "T00:00:00+00:00")
    # from the UTC midnight before local midnight until the one after, then keep the local calendar day
    instants = [start - timedelta(hours=2) + timedelta(minutes=15 * i) for i in range(4 * 28)]
    return [_row(timestamp=t.strftime("%Y-%m-%dT%H:%M:%SZ")) for t in instants]


@pytest.mark.parametrize(("date", "quarter_hours"), [("2026-03-29", 92), ("2026-10-25", 100)])
def test_whole_dst_day(date: str, quarter_hours: int) -> None:
    rows = [r for r in _day(date) if r.timestamp.astimezone(ZoneInfo("Europe/Berlin")).date().isoformat() == date]
    frame = mp.to_frame(rows)
    assert frame.height == quarter_hours
    assert frame["timestamp"].n_unique() == quarter_hours
    assert frame["timestamp"].dt.epoch("s").n_unique() == quarter_hours
    assert mp.from_frame(frame) == rows


def test_round_trip_all_fixtures_in_one_frame_and_berlin_time() -> None:
    rows = [Messwert.model_validate_json(p.read_bytes()) for p in FIXTURES]
    rows = [r for r in rows if r.value is None or abs(r.value) < LIMIT]
    assert mp.from_frame(mp.to_frame(rows)) == rows
    # first and last quarter hour of the spring DST day: local 00:00 (UTC+1) and 23:45 (UTC+2)
    frame = mp.to_frame([_row(timestamp="2026-03-28T23:00:00Z"), _row(timestamp="2026-03-29T21:45:00Z")])
    assert [(t.day, t.hour, t.minute) for t in frame["timestamp"]] == [(29, 0, 0), (29, 23, 45)]
    # the repeated hour on the autumn day stays distinct
    frame = mp.to_frame([_row(timestamp="2026-10-25T00:00:00Z"), _row(timestamp="2026-10-25T01:00:00Z")])
    assert [t.hour for t in frame["timestamp"]] == [2, 2]
    # (Python compares same-zone datetimes without the fold, so compare the instants)
    assert frame["timestamp"].dt.epoch("s").to_list() == [1792886400, 1792890000]


def test_empty_rows() -> None:
    frame = mp.to_frame([])
    assert frame.height == 0
    assert dict(frame.schema) == mp.CANONICAL_DTYPES
    assert mp.from_frame(frame) == []


@pytest.mark.parametrize(
    "value", ["999999999.999999", "-999999999.999999", "0.000001", "0.1", "0.3", "123456789.123456"]
)
def test_below_the_bound_round_trips(value: str) -> None:
    row = _row(value)
    assert mp.from_frame(mp.to_frame([row])) == [row]


@pytest.mark.parametrize("value", ["1000000000", "-1000000000", "999999999999.999999"])
def test_at_or_above_the_bound_raises(value: str) -> None:
    with pytest.raises(ValueError, match="10"):
        mp.to_frame([_row(value)])


def test_thirteen_integer_digits_raise() -> None:
    # The model itself refuses 1234567890123.45 (13 integer digits), so build the row without validation to show
    # that to_frame guards the bound on its own, whoever constructed the row.
    row = _row().model_copy(update={"value": Decimal("1234567890123.45")})
    with pytest.raises(ValueError, match="10"):
        mp.to_frame([row])


def test_why_repr_is_used() -> None:
    # Decimal(float) exposes the binary expansion, which is why from_frame goes through repr
    assert str(Decimal(1234567890123.45)) != "1234567890123.45"  # noqa: RUF032
    assert str(Decimal(0.1)) != "0.1"  # noqa: RUF032
    frame = mp.to_frame([_row("1.5")]).with_columns(value=pl.lit(0.1))
    assert mp.from_frame(frame)[0].value == Decimal("0.1")


def test_from_frame_quantizes_half_even_and_normalises() -> None:
    frame = mp.to_frame([_row("1"), _row("2"), _row("3")]).with_columns(value=pl.Series([0.0000005, 0.0000015, 1.50]))
    assert [r.value for r in mp.from_frame(frame)] == [Decimal("0"), Decimal("0.000002"), Decimal("1.5")]
    assert [str(r.value) for r in mp.from_frame(frame)] == ["0", "0.000002", "1.5"]


def test_to_frame_accepts_an_iterator() -> None:
    rows = [_row(), _row(timestamp="2026-01-15T10:30:00Z")]
    frame = mp.to_frame(row for row in rows)
    assert frame.height == 2
    assert mp.from_frame(frame) == rows


def test_from_frame_rejects_bad_frames() -> None:
    frame = mp.to_frame([_row()])
    with pytest.raises(ValueError, match="missing columns"):
        mp.from_frame(frame.drop("status"))
    with pytest.raises(ValueError, match="time zone"):
        mp.from_frame(frame.with_columns(pl.col("timestamp").dt.replace_time_zone(None)))
    with pytest.raises(ValueError, match="finite"):
        mp.from_frame(frame.with_columns(value=pl.lit(float("inf"))))
    with pytest.raises(TypeError, match="LazyFrame"):
        mp.from_frame(frame.lazy())


def test_from_frame_checks_the_dtype_of_every_column() -> None:
    frame = mp.to_frame([_row()])
    bad = {
        "value": [pl.lit("1.5"), pl.lit(True), pl.lit(Decimal("1.500000")).cast(pl.Decimal(20, 6)), pl.lit(1)],
        "malo": [pl.lit(51238696781)],
        "status": [pl.lit(1)],
        "timestamp": [pl.lit("2026-01-15T10:15:00Z"), pl.col("timestamp").dt.cast_time_unit("ns")],
    }
    for column, replacements in bad.items():
        for replacement in replacements:
            with pytest.raises(ValueError, match=rf"column '{column}' has dtype .*expected"):
                mp.from_frame(frame.with_columns(replacement.alias(column)))


def test_from_frame_accepts_all_null_columns_of_dtype_null() -> None:
    row = _row(None)
    frame = mp.to_frame([row]).with_columns(value=pl.lit(None), herkunft=pl.lit(None))
    assert frame.schema["value"] == pl.Null
    assert mp.from_frame(frame) == [row]


def test_from_frame_rejects_extra_columns_including_resolution() -> None:
    frame = mp.to_frame([_row()])
    with pytest.raises(ValueError, match="unexpected columns: \\['extra'\\]"):
        mp.from_frame(frame.with_columns(extra=pl.lit(1)))
    with pytest.raises(ValueError, match="resolution"):
        mp.from_frame(frame.with_columns(resolution=pl.lit("PT15M")))


def test_from_frame_applies_the_same_bound_as_to_frame() -> None:
    frame = mp.to_frame([_row()])
    assert mp.from_frame(frame.with_columns(value=pl.lit(999999999.999999)))[0].value == Decimal("999999999.999999")
    for too_big in (1e9, -1e9, 123456789012.0):
        with pytest.raises(ValueError, match="10"):
            mp.from_frame(frame.with_columns(value=pl.lit(too_big)))


def test_from_frame_converts_other_time_zones() -> None:
    frame = mp.to_frame([_row()])
    other = frame.with_columns(pl.col("timestamp").dt.convert_time_zone("Asia/Tokyo"))
    assert mp.from_frame(other) == mp.from_frame(frame)


def test_null_value_and_herkunft_stay_null() -> None:
    row = _row(None)
    frame = mp.to_frame([row])
    assert frame["value"][0] is None
    assert frame["herkunft"][0] is None
    assert mp.from_frame(frame) == [row]


@pytest.fixture
def hostile_context() -> Iterator[None]:
    """An ambient decimal context that would break careless code: 5 digits of precision, Inexact trapped."""
    with localcontext(Context(prec=5, traps=[InvalidOperation, Inexact, Rounded])):
        yield


@pytest.mark.usefixtures("hostile_context")
def test_both_directions_ignore_the_ambient_decimal_context() -> None:
    row = _row("999999999.999999")
    frame = mp.to_frame([row])
    assert mp.from_frame(frame) == [row]
    assert mp.from_frame(frame.with_columns(value=pl.lit(0.1234565)))[0].value == Decimal("0.123456")  # half-even
    with pytest.raises(ValueError, match="10"):
        mp.to_frame([_row("1000000000")])
    with pytest.raises(ValueError, match="10"):
        mp.from_frame(frame.with_columns(value=pl.lit(1e9)))


@pytest.mark.parametrize("bad", [float("nan"), float("inf"), float("-inf")])
def test_from_frame_non_finite_floats_raise_value_error(bad: float) -> None:
    frame = mp.to_frame([_row()]).with_columns(value=pl.lit(bad))
    with pytest.raises(ValueError, match="finite") as info:
        mp.from_frame(frame)
    assert not isinstance(info.value, ArithmeticError)


def test_from_frame_rejects_nanosecond_timestamps() -> None:
    # 1 ns after the quarter-hour boundary must not be truncated to the boundary
    frame = mp.to_frame([_row()])
    ns = frame.with_columns(pl.col("timestamp").dt.cast_time_unit("ns") + pl.duration(nanoseconds=1))
    with pytest.raises(ValueError, match=r"column 'timestamp' has dtype .*ns"):
        mp.from_frame(ns)
